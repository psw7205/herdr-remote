package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/coder/websocket"
	"herdr-remote/internal/command"
	"herdr-remote/internal/herdr"
	"herdr-remote/internal/session"
	"herdr-remote/internal/stream"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Sessions interface {
	List() []session.Meta
	Get(string) (stream.Snapshot, session.Meta, error)
	Stream(string) (*stream.Session, error)
	BoundInput(context.Context, string, string, string, string) error
	Terminal(context.Context, string, string) (herdr.TerminalSnapshot, error)
}
type Server struct {
	registry     Sessions
	receipts     *command.Store
	origins      []string
	static       http.Handler
	tailnetHost  string
	tailnetLogin string
}

func New(registry Sessions, receipts *command.Store, origins []string, staticDir string) *Server {
	var files http.Handler
	if staticDir != "" {
		files = http.FileServer(http.Dir(staticDir))
	}
	return &Server{registry: registry, receipts: receipts, origins: origins, static: files}
}
func (s *Server) SetTailnetIdentity(host, login string) {
	s.tailnetHost = strings.TrimSuffix(host, ".")
	s.tailnetLogin = login
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/sessions", s.sessions)
	mux.HandleFunc("GET /api/sessions/{id}", s.snapshot)
	mux.HandleFunc("GET /api/sessions/{id}/events", s.events)
	mux.HandleFunc("GET /api/sessions/{id}/terminal", s.terminal)
	mux.HandleFunc("POST /api/sessions/{id}/commands", s.command)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if s.static == nil {
			http.NotFound(w, r)
			return
		}
		s.static.ServeHTTP(w, r)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		localHost := strings.HasPrefix(r.Host, "127.0.0.1:") || strings.HasPrefix(r.Host, "localhost:") || strings.HasPrefix(r.Host, "[::1]:")
		remoteHost := s.tailnetHost != "" && (r.Host == s.tailnetHost || r.Host == s.tailnetHost+":443")
		if !localHost && !remoteHost {
			problem(w, 403, "HOST_REJECTED")
			return
		}
		remoteOrigin := s.tailnetHost != "" && r.Header.Get("Origin") == "https://"+s.tailnetHost
		forwardedHost := r.Header.Get("X-Forwarded-Host")
		remoteProxy := s.tailnetHost != "" && (forwardedHost == s.tailnetHost || r.Header.Get("X-Forwarded-Proto") == "https")
		if remoteHost || remoteOrigin || remoteProxy {
			if s.tailnetLogin == "" || !strings.EqualFold(r.Header.Get("Tailscale-User-Login"), s.tailnetLogin) {
				problem(w, 403, "TAILNET_IDENTITY_REJECTED")
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}
func jsonResponse(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func problem(w http.ResponseWriter, status int, code string) {
	jsonResponse(w, status, map[string]string{"error": code})
}
func (s *Server) sessions(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, 200, map[string]any{"sessions": s.registry.List()})
}
func (s *Server) snapshot(w http.ResponseWriter, r *http.Request) {
	snapshot, meta, err := s.registry.Get(r.PathValue("id"))
	if err != nil {
		problem(w, 404, "SESSION_NOT_FOUND")
		return
	}
	jsonResponse(w, 200, map[string]any{"session": meta, "snapshot": snapshot})
}
func (s *Server) terminal(w http.ResponseWriter, r *http.Request) {
	frame, err := s.registry.Terminal(r.Context(), r.PathValue("id"), r.Header.Get("X-Runtime-Binding"))
	if err != nil {
		problem(w, 409, "SESSION_CHANGED")
		return
	}
	jsonResponse(w, 200, frame)
}
func originAllowed(r *http.Request, origins []string) bool {
	origin := r.Header.Get("Origin")
	if origin == "" || origin == "null" {
		return false
	}
	for _, allowed := range origins {
		if origin != allowed {
			continue
		}
		parsed, err := url.Parse(allowed)
		if err != nil || parsed.Host == "" || parsed.Scheme == "" {
			return false
		}
		if r.Host == parsed.Host || strings.HasPrefix(r.Host, "127.0.0.1:") || strings.HasPrefix(r.Host, "localhost:") {
			return true
		}
	}
	return false
}
func (s *Server) command(w http.ResponseWriter, r *http.Request) {
	if !originAllowed(r, s.origins) {
		problem(w, 403, "ORIGIN_REJECTED")
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		problem(w, 415, "JSON_REQUIRED")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 72<<10)
	var req command.Request
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&req); err != nil {
		problem(w, 400, "INVALID_COMMAND")
		return
	}
	var extra any
	if err = decoder.Decode(&extra); err != io.EOF {
		problem(w, 400, "INVALID_COMMAND")
		return
	}
	if req.SessionID != r.PathValue("id") || req.RuntimeBinding == "" || req.Type != "prompt" && req.Type != "interrupt" && req.Type != "terminal_input" || req.Type == "prompt" && strings.TrimSpace(req.Payload.Text) == "" || len(req.Payload.Text) > 65536 {
		problem(w, 400, "INVALID_COMMAND")
		return
	}
	result := s.receipts.Execute(r.Context(), req, func(ctx context.Context, req command.Request) command.Result {
		err := s.registry.BoundInput(ctx, req.SessionID, req.RuntimeBinding, req.Type, req.Payload.Text)
		if err == nil {
			return command.Result{Status: "accepted"}
		}
		var api *herdr.APIError
		if errors.As(err, &api) {
			switch api.Code {
			case "runtime_binding_mismatch", "session_ended", "agent_not_ready", "invalid_prompt", "invalid_input", "input_rejected":
				return command.Result{Status: "rejected", Code: strings.ToUpper(api.Code)}
			case "delivery_unknown":
				return command.Result{Status: "delivery_unknown", Code: "DELIVERY_UNKNOWN"}
			}
		}
		if err.Error() == "chat transcript unavailable" {
			return command.Result{Status: "rejected", Code: "CHAT_UNAVAILABLE"}
		}
		if err.Error() == "session binding changed" || err.Error() == "session not found" || err.Error() == "agent is not ready for a chat prompt" || err.Error() == "terminal unavailable" {
			return command.Result{Status: "rejected", Code: "SESSION_CHANGED"}
		}
		slog.Warn("bound input delivery uncertain", "error", err)
		return command.Result{Status: "delivery_unknown", Code: "DELIVERY_UNKNOWN"}
	})
	switch result.Status {
	case "accepted":
		jsonResponse(w, 202, result)
	case "rejected":
		jsonResponse(w, 409, result)
	default:
		jsonResponse(w, 202, result)
	}
}
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	if !originAllowed(r, s.origins) {
		problem(w, 403, "ORIGIN_REJECTED")
		return
	}
	hub, err := s.registry.Stream(r.PathValue("id"))
	if err != nil {
		problem(w, 404, "SESSION_NOT_FOUND")
		return
	}
	sequence, e := strconv.ParseUint(r.URL.Query().Get("sequence"), 10, 64)
	if e != nil {
		sequence = 0
	}
	cursor := stream.Cursor{Epoch: r.URL.Query().Get("epoch"), Sequence: sequence}
	replay, live, closeSubscription := hub.Subscribe(cursor)
	defer closeSubscription()
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer conn.CloseNow()
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go func() {
		for {
			if _, _, err := conn.Read(ctx); err != nil {
				cancel()
				return
			}
		}
	}()
	send := func(e stream.Event) error {
		writeCtx, stop := context.WithTimeout(ctx, 10*time.Second)
		defer stop()
		b, err := json.Marshal(e)
		if err != nil {
			return err
		}
		return conn.Write(writeCtx, websocket.MessageText, b)
	}
	for _, event := range replay {
		if send(event) != nil {
			return
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-live:
			if !ok {
				return
			}
			if send(event) != nil {
				return
			}
		}
	}
}
func (s *Server) ValidateOrigins() error {
	if len(s.origins) == 0 {
		return errors.New("at least one exact Origin is required")
	}
	for _, origin := range s.origins {
		u, e := url.Parse(origin)
		if e != nil || u.Scheme != "https" && u.Scheme != "http" || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("invalid allowed Origin %q", origin)
		}
		if u.Scheme == "https" && u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" && (s.tailnetHost == "" || s.tailnetLogin == "" || u.Hostname() != s.tailnetHost) {
			return errors.New("tailnet HTTPS Origin requires the matching host and owner login")
		}
	}
	return nil
}
