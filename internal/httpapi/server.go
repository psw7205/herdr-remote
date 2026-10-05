package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/coder/websocket"
	"github.com/psw7205/herdr-remote/internal/command"
	"github.com/psw7205/herdr-remote/internal/gitstate"
	"github.com/psw7205/herdr-remote/internal/herdr"
	"github.com/psw7205/herdr-remote/internal/session"
	"github.com/psw7205/herdr-remote/internal/stream"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

type Sessions interface {
	List() []session.Meta
	Get(string) (stream.Snapshot, session.Meta, error)
	Stream(string) (*stream.Session, error)
	Project(string) (string, error)
	BoundInput(context.Context, string, string, string, string) error
	Terminal(context.Context, string, string) (herdr.TerminalSnapshot, error)
	ConditionalInput() string
}

// Starter asks Herdr for a new session (ADR-037). A nil Starter disables it.
type Starter interface {
	Candidates(context.Context) ([]session.Candidate, error)
	Start(ctx context.Context, commandID, candidateID, kind, placement string) session.StartResult
	Kinds() []string
	NewWorkspace() bool
}

// ChangeReader reads a project's Git working tree (ADR-016). A nil
// ChangeReader disables the Changed Files routes.
type ChangeReader interface {
	Changes(ctx context.Context, dir string) (gitstate.Changes, error)
	Diff(ctx context.Context, dir, path string) (gitstate.Diff, error)
}
type Server struct {
	registry     Sessions
	starter      Starter
	changes      ChangeReader
	receipts     *command.Store
	origins      []string
	static       http.Handler
	staticDir    string
	build        Build
	tailnetHost  string
	tailnetLogin string
}

func New(registry Sessions, receipts *command.Store, origins []string, staticDir string) *Server {
	var files http.Handler
	if staticDir != "" {
		files = http.FileServer(noListingFS{http.Dir(staticDir)})
	}
	return &Server{registry: registry, receipts: receipts, origins: origins, static: files, staticDir: staticDir}
}
func (s *Server) SetStarter(starter Starter)     { s.starter = starter }
func (s *Server) SetChanges(reader ChangeReader) { s.changes = reader }
func (s *Server) SetTailnetIdentity(host, login string) {
	s.tailnetHost = strings.TrimSuffix(host, ".")
	s.tailnetLogin = login
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/sessions", s.sessions)
	mux.HandleFunc("POST /api/sessions", s.start)
	mux.HandleFunc("GET /api/start-candidates", s.candidates)
	mux.HandleFunc("GET /api/sessions/{id}", s.snapshot)
	mux.HandleFunc("GET /api/sessions/{id}/events", s.events)
	mux.HandleFunc("GET /api/sessions/{id}/terminal", s.terminal)
	mux.HandleFunc("POST /api/sessions/{id}/commands", s.command)
	mux.HandleFunc("GET /api/sessions/{id}/changes", s.changedFiles)
	mux.HandleFunc("GET /api/sessions/{id}/changes/diff", s.changedFileDiff)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if s.static == nil {
			http.NotFound(w, r)
			return
		}
		s.static.ServeHTTP(w, r)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setSecurityHeaders(w.Header())
		localHost := strings.HasPrefix(r.Host, "127.0.0.1:") || strings.HasPrefix(r.Host, "localhost:") || strings.HasPrefix(r.Host, "[::1]:")
		remoteHost := s.tailnetHost != "" && (r.Host == s.tailnetHost || r.Host == s.tailnetHost+":443")
		if !localHost && !remoteHost {
			problem(w, 403, "HOST_REJECTED")
			return
		}
		remoteOrigin := s.tailnetHost != "" && r.Header.Get("Origin") == "https://"+s.tailnetHost
		// Serve forwards the client's Host header unchanged, so a peer can forge a
		// loopback Host. The headers Serve adds mark a proxied request regardless
		// of Host and of whether a tailnet host was resolved at startup.
		if remoteHost || remoteOrigin || proxied(r) {
			if s.tailnetHost == "" || s.tailnetLogin == "" || !strings.EqualFold(r.Header.Get("Tailscale-User-Login"), s.tailnetLogin) {
				problem(w, 403, "TAILNET_IDENTITY_REJECTED")
				return
			}
			if !remoteHost {
				problem(w, 403, "HOST_REJECTED")
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}

// proxyHeaders are added by Tailscale Serve. A direct local browser request,
// including the Vite dev proxy (no xfwd), carries none of them.
var proxyHeaders = []string{"X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "Tailscale-User-Login", "Tailscale-User-Name"}

func proxied(r *http.Request) bool {
	for _, name := range proxyHeaders {
		if len(r.Header.Values(name)) > 0 {
			return true
		}
	}
	return false
}
func setSecurityHeaders(h http.Header) {
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Content-Security-Policy", "frame-ancestors 'none'")
}

// noListingFS hides directories without index.html so http.FileServer returns
// 404 instead of a directory listing.
type noListingFS struct{ http.FileSystem }

func (f noListingFS) Open(name string) (http.File, error) {
	file, err := f.FileSystem.Open(name)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	if info.IsDir() {
		index, err := f.FileSystem.Open(path.Join(name, "index.html"))
		if err != nil {
			file.Close()
			return nil, fs.ErrNotExist
		}
		index.Close()
	}
	return file, nil
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
	jsonResponse(w, 200, map[string]any{"sessions": s.registry.List(), "herdr": map[string]string{"conditional_input": s.registry.ConditionalInput()}, "start": s.startCapability(), "bridge": s.Build()})
}

// startCapability tells the client which agent kinds it may start and
// whether a new workspace is possible (only with a -project-root).
type startCapability struct {
	Kinds        []string `json:"kinds"`
	NewWorkspace bool     `json:"new_workspace"`
}

func (s *Server) startCapability() startCapability {
	if s.starter == nil {
		return startCapability{Kinds: []string{}}
	}
	return startCapability{Kinds: s.starter.Kinds(), NewWorkspace: s.starter.NewWorkspace()}
}
func (s *Server) candidates(w http.ResponseWriter, r *http.Request) {
	if s.starter == nil {
		problem(w, 404, "START_UNAVAILABLE")
		return
	}
	candidates, err := s.starter.Candidates(r.Context())
	if err != nil {
		problem(w, 503, "HERDR_UNAVAILABLE")
		return
	}
	if candidates == nil {
		candidates = []session.Candidate{}
	}
	jsonResponse(w, 200, map[string]any{"candidates": candidates, "start": s.startCapability()})
}

// startTimeout bounds one session_start dispatch: create (5s), a shell-busy
// retry (2s) and Herdr's 30s agent readiness wait, with margin. The server has
// no WriteTimeout, so this is the longest a start holds its request.
const startTimeout = 50 * time.Second

var candidateIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

type startRequest struct {
	CommandID string `json:"command_id"`
	Type      string `json:"command_type"`
	Payload   struct {
		CandidateID string `json:"candidate_id"`
		Kind        string `json:"kind"`
		Placement   string `json:"placement"`
	} `json:"payload"`
}

func (s *Server) start(w http.ResponseWriter, r *http.Request) {
	if !originAllowed(r, s.origins) {
		problem(w, 403, "ORIGIN_REJECTED")
		return
	}
	if s.starter == nil {
		problem(w, 404, "START_UNAVAILABLE")
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		problem(w, 415, "JSON_REQUIRED")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	var body startRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&body); err != nil {
		problem(w, 400, "INVALID_COMMAND")
		return
	}
	var extra any
	if err = decoder.Decode(&extra); err != io.EOF {
		problem(w, 400, "INVALID_COMMAND")
		return
	}
	p := body.Payload
	if body.Type != "session_start" || !candidateIDPattern.MatchString(p.CandidateID) || !slices.Contains(s.starter.Kinds(), p.Kind) || p.Placement != session.PlacementNewWorkspace && p.Placement != session.PlacementNewTab {
		problem(w, 400, "INVALID_COMMAND")
		return
	}
	req := command.Request{CommandID: body.CommandID, Type: body.Type, Payload: command.Payload{CandidateID: p.CandidateID, Kind: p.Kind, Placement: p.Placement}}
	// The dispatch outlives the request: a client that gives up must not
	// strand a pending receipt while Herdr is still creating the session.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), startTimeout)
	defer cancel()
	result := s.receipts.Execute(ctx, req, func(ctx context.Context, req command.Request) command.Result {
		out := s.starter.Start(ctx, req.CommandID, req.Payload.CandidateID, req.Payload.Kind, req.Payload.Placement)
		result := command.Result{Status: out.Status, Code: out.Code}
		if out.Created != nil {
			result.Created = &command.Created{WorkspaceID: out.Created.WorkspaceID, TabID: out.Created.TabID, PaneID: out.Created.PaneID, Agent: out.Agent}
		}
		return result
	})
	if result.Status == "rejected" {
		jsonResponse(w, 409, result)
		return
	}
	jsonResponse(w, 202, result)
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

// changesTimeout bounds one Changed Files request: a few git runs of at most
// 10s each, behind at most two other requests. The server has no WriteTimeout.
const changesTimeout = 30 * time.Second

// maxDiffPath bounds the path query; git paths are far shorter (PATH_MAX).
const maxDiffPath = 4096

func (s *Server) changedFiles(w http.ResponseWriter, r *http.Request) {
	dir, ok := s.changesProject(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), changesTimeout)
	defer cancel()
	changes, err := s.changes.Changes(ctx, dir)
	if err != nil {
		changesProblem(w, err)
		return
	}
	jsonResponse(w, 200, changes)
}
func (s *Server) changedFileDiff(w http.ResponseWriter, r *http.Request) {
	dir, ok := s.changesProject(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	path := query.Get("path")
	if len(query["path"]) != 1 || path == "" || len(path) > maxDiffPath || strings.ContainsRune(path, 0) {
		problem(w, 400, "INVALID_PATH")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), changesTimeout)
	defer cancel()
	diff, err := s.changes.Diff(ctx, dir, path)
	if err != nil {
		changesProblem(w, err)
		return
	}
	jsonResponse(w, 200, diff)
}

// changesProject resolves the item's directory from the registry; the client
// never names a directory. A same-origin GET carries no Origin header, so only
// a present Origin is checked: a cross-site page reading source is refused
// before any git run, not just left unable to read the response.
func (s *Server) changesProject(w http.ResponseWriter, r *http.Request) (string, bool) {
	if r.Header.Get("Origin") != "" && !originAllowed(r, s.origins) {
		problem(w, 403, "ORIGIN_REJECTED")
		return "", false
	}
	if s.changes == nil {
		problem(w, 404, "CHANGES_UNAVAILABLE")
		return "", false
	}
	dir, err := s.registry.Project(r.PathValue("id"))
	if err != nil {
		problem(w, 404, "SESSION_NOT_FOUND")
		return "", false
	}
	return dir, true
}
func changesProblem(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, gitstate.ErrUnavailable):
		problem(w, 503, "GIT_UNAVAILABLE")
	case errors.Is(err, gitstate.ErrProjectMissing):
		problem(w, 404, "PROJECT_NOT_FOUND")
	case errors.Is(err, gitstate.ErrNotRepository):
		problem(w, 404, "NOT_A_REPOSITORY")
	case errors.Is(err, gitstate.ErrNotChanged):
		problem(w, 404, "FILE_NOT_CHANGED")
	case errors.Is(err, context.DeadlineExceeded):
		problem(w, 504, "GIT_TIMEOUT")
	default:
		slog.Warn("git read failed", "error", err)
		problem(w, 500, "GIT_FAILED")
	}
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
	// session_start fields share the receipt Payload but never belong here.
	if req.Payload.CandidateID != "" || req.Payload.Kind != "" || req.Payload.Placement != "" {
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
		// Herdr rejects an unknown method while deserializing the request, before
		// any dispatch or PTY write, so this is a definite non-delivery.
		if errors.Is(err, herdr.ErrUnsupported) {
			return command.Result{Status: "rejected", Code: "HERDR_UNSUPPORTED"}
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
		if err.Error() == "agent is not ready for a chat prompt" {
			return command.Result{Status: "rejected", Code: "AGENT_NOT_READY"}
		}
		if err.Error() == "session binding changed" || err.Error() == "session not found" || err.Error() == "terminal unavailable" {
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
