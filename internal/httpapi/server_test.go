package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/coder/websocket"
	"github.com/psw7205/herdr-remote/internal/command"
	"github.com/psw7205/herdr-remote/internal/herdr"
	"github.com/psw7205/herdr-remote/internal/session"
	"github.com/psw7205/herdr-remote/internal/stream"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMutationRequiresExactOriginAndHost(t *testing.T) {
	for _, tc := range []struct {
		origin, host string
		allow        bool
	}{
		{"https://private.ts.net", "private.ts.net", true},
		{"https://evil.example", "private.ts.net", false},
		{"null", "private.ts.net", false},
		{"", "private.ts.net", false},
		{"https://private.ts.net", "evil.example", false},
	} {
		r := httptest.NewRequest(http.MethodPost, "http://private.ts.net/api/sessions/x/commands", nil)
		r.Host = tc.host
		if tc.origin != "" {
			r.Header.Set("Origin", tc.origin)
		}
		got := originAllowed(r, []string{"https://private.ts.net"})
		if got != tc.allow {
			t.Fatalf("origin %q host %q allowed=%v", tc.origin, tc.host, got)
		}
	}
}

type fakeSessions struct {
	hub         *stream.Session
	count       atomic.Int32
	conditional string
	boundErr    error
}

func (f *fakeSessions) ConditionalInput() string { return f.conditional }

func (f *fakeSessions) List() []session.Meta {
	return []session.Meta{{ID: "claude:native", Binding: "bound", Chat: true, Active: true}}
}
func (f *fakeSessions) Get(string) (stream.Snapshot, session.Meta, error) {
	return f.hub.Snapshot(), f.List()[0], nil
}
func (f *fakeSessions) Stream(string) (*stream.Session, error) { return f.hub, nil }
func (f *fakeSessions) BoundInput(_ context.Context, _, binding, _, _ string) error {
	if binding != "bound" {
		return errors.New("session binding changed")
	}
	f.count.Add(1)
	return f.boundErr
}
func (f *fakeSessions) Terminal(context.Context, string, string) (herdr.TerminalSnapshot, error) {
	return herdr.TerminalSnapshot{}, nil
}
func newTestServer(t *testing.T) (*httptest.Server, *fakeSessions) {
	t.Helper()
	store, e := command.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	sessions := &fakeSessions{hub: stream.New("claude:native")}
	server := httptest.NewServer(New(sessions, store, []string{"http://localhost:5173"}, "").Handler())
	t.Cleanup(server.Close)
	return server, sessions
}
func TestCommandRetryAndStaleBinding(t *testing.T) {
	server, sessions := newTestServer(t)
	id := "00000000-0000-4000-8000-000000000001"
	commandBody := func(binding string) string {
		return fmt.Sprintf(`{"command_id":%q,"session_id":"claude:native","runtime_binding":%q,"command_type":"prompt","payload":{"text":"hello"}}`, id, binding)
	}
	send := func(body string) *http.Response {
		r, e := http.NewRequest(http.MethodPost, server.URL+"/api/sessions/claude:native/commands", strings.NewReader(body))
		if e != nil {
			t.Fatal(e)
		}
		r.Header.Set("Origin", "http://localhost:5173")
		r.Header.Set("Content-Type", "application/json")
		res, e := http.DefaultClient.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		res.Body.Close()
		return res
	}
	if got := send(commandBody("bound")).StatusCode; got != 202 {
		t.Fatal(got)
	}
	if got := send(commandBody("bound")).StatusCode; got != 202 {
		t.Fatal(got)
	}
	if sessions.count.Load() != 1 {
		t.Fatalf("double dispatch: %d", sessions.count.Load())
	}
	if got := send(commandBody("stale")).StatusCode; got != 409 {
		t.Fatal(got)
	}
	if sessions.count.Load() != 1 {
		t.Fatal("stale dispatch")
	}
}
func TestUnsupportedBoundInputIsDurablyRejected(t *testing.T) {
	server, sessions := newTestServer(t)
	sessions.boundErr = fmt.Errorf("%w: agent.bound_input", herdr.ErrUnsupported)
	body := `{"command_id":"00000000-0000-4000-8000-000000000002","session_id":"claude:native","runtime_binding":"bound","command_type":"prompt","payload":{"text":"hello"}}`
	for attempt := range 2 {
		r, e := http.NewRequest(http.MethodPost, server.URL+"/api/sessions/claude:native/commands", strings.NewReader(body))
		if e != nil {
			t.Fatal(e)
		}
		r.Header.Set("Origin", "http://localhost:5173")
		r.Header.Set("Content-Type", "application/json")
		res, e := http.DefaultClient.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		var result command.Result
		e = json.NewDecoder(res.Body).Decode(&result)
		res.Body.Close()
		if e != nil || res.StatusCode != 409 || result != (command.Result{Status: "rejected", Code: "HERDR_UNSUPPORTED"}) {
			t.Fatalf("attempt %d: status %d result %+v err %v", attempt, res.StatusCode, result, e)
		}
	}
	if sessions.count.Load() != 1 {
		t.Fatalf("retry re-dispatched: %d", sessions.count.Load())
	}
}
func TestSessionsReportsHerdrConditionalInput(t *testing.T) {
	server, sessions := newTestServer(t)
	for _, want := range []string{"supported", "unsupported", "unknown"} {
		sessions.conditional = want
		res, e := http.Get(server.URL + "/api/sessions")
		if e != nil {
			t.Fatal(e)
		}
		var body struct {
			Sessions []session.Meta `json:"sessions"`
			Herdr    map[string]any `json:"herdr"`
		}
		e = json.NewDecoder(res.Body).Decode(&body)
		res.Body.Close()
		if e != nil || res.StatusCode != 200 {
			t.Fatal(res.StatusCode, e)
		}
		if len(body.Sessions) != 1 || body.Sessions[0].ID != "claude:native" {
			t.Fatalf("sessions changed: %+v", body.Sessions)
		}
		if len(body.Herdr) != 1 || body.Herdr["conditional_input"] != want {
			t.Fatalf("herdr = %+v, want %s", body.Herdr, want)
		}
	}
}
func TestWebSocketReplaysSnapshotGapAndDisconnectKeepsSession(t *testing.T) {
	server, sessions := newTestServer(t)
	snap := sessions.hub.Snapshot()
	sessions.hub.Append("message.user", map[string]string{"text": "between"})
	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/sessions/claude:native/events?epoch=" + snap.Cursor.Epoch + "&sequence=0"
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, res, e := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{"http://localhost:5173"}}})
	if e != nil {
		t.Fatal(e, res)
	}
	_, b, e := conn.Read(ctx)
	if e != nil {
		t.Fatal(e)
	}
	var event stream.Event
	if e = json.Unmarshal(b, &event); e != nil || event.Type != "message.user" {
		t.Fatalf("wrong replay: %+v %v", event, e)
	}
	conn.Close(websocket.StatusNormalClosure, "done")
	sessions.hub.Append("message.assistant", map[string]string{"text": "continued"})
	if sessions.hub.Snapshot().Cursor.Sequence != 2 {
		t.Fatal("disconnect ended session")
	}
}

func TestTailnetHostRequiresServeOwnerIdentity(t *testing.T) {
	store, e := command.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	fake := &fakeSessions{hub: stream.New("session")}
	server := New(fake, store, []string{"http://127.0.0.1:8787", "https://node.tailnet.ts.net"}, "")
	server.SetTailnetIdentity("node.tailnet.ts.net", "owner@example.com")
	for _, tc := range []struct {
		host, login string
		want        int
	}{
		{"node.tailnet.ts.net", "", 403},
		{"node.tailnet.ts.net", "other@example.com", 403},
		{"node.tailnet.ts.net", "owner@example.com", 200},
		{"127.0.0.1:8787", "", 200},
	} {
		req := httptest.NewRequest(http.MethodGet, "http://"+tc.host+"/api/sessions", nil)
		if tc.login != "" {
			req.Header.Set("Tailscale-User-Login", tc.login)
		}
		out := httptest.NewRecorder()
		server.Handler().ServeHTTP(out, req)
		if out.Code != tc.want {
			t.Fatalf("host %s login %s: %d", tc.host, tc.login, out.Code)
		}
	}
}
func TestRemoteOriginRequiresIdentityEvenWithLoopbackProxyHost(t *testing.T) {
	store, _ := command.Open(t.TempDir())
	fake := &fakeSessions{hub: stream.New("session")}
	server := New(fake, store, []string{"https://node.tailnet.ts.net"}, "")
	server.SetTailnetIdentity("node.tailnet.ts.net", "owner@example.com")
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8787/api/sessions/session/commands", strings.NewReader("{}"))
	req.Header.Set("Origin", "https://node.tailnet.ts.net")
	req.Header.Set("Content-Type", "application/json")
	out := httptest.NewRecorder()
	server.Handler().ServeHTTP(out, req)
	if out.Code != 403 {
		t.Fatalf("missing Serve identity was accepted: %d", out.Code)
	}
}

func gateServer(t *testing.T, host, login, static string) http.Handler {
	t.Helper()
	store, e := command.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	server := New(&fakeSessions{hub: stream.New("session")}, store, []string{"http://127.0.0.1:8787"}, static)
	server.SetTailnetIdentity(host, login)
	return server.Handler()
}
func serveGate(h http.Handler, method, host string, header map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "http://"+host+"/api/sessions", nil)
	for name, value := range header {
		req.Header.Set(name, value)
	}
	out := httptest.NewRecorder()
	h.ServeHTTP(out, req)
	return out
}
func problemCode(t *testing.T, out *httptest.ResponseRecorder) string {
	t.Helper()
	var body map[string]string
	if e := json.Unmarshal(out.Body.Bytes(), &body); e != nil {
		t.Fatalf("status %d body %q: %v", out.Code, out.Body.String(), e)
	}
	return body["error"]
}

// Serve forwards the client's Host unchanged, so a tailnet peer can send a
// loopback Host. The headers Serve adds must still mark the request as remote.
func TestForgedLoopbackHostWithServeHeadersIsRejected(t *testing.T) {
	forged := map[string]string{
		"X-Forwarded-For":      "100.64.0.2",
		"X-Forwarded-Host":     "127.0.0.1:8787",
		"X-Forwarded-Proto":    "https",
		"Tailscale-User-Login": "peer@example.com",
		"Tailscale-User-Name":  "Peer",
	}
	for _, host := range []string{"", "node.tailnet.ts.net"} {
		h := gateServer(t, host, "owner@example.com", "")
		for name, value := range forged {
			for _, method := range []string{http.MethodGet, http.MethodPost} {
				out := serveGate(h, method, "127.0.0.1:8787", map[string]string{name: value, "Origin": "http://127.0.0.1:8787"})
				if out.Code != 403 || problemCode(t, out) != "TAILNET_IDENTITY_REJECTED" {
					t.Fatalf("tailnet host %q: %s with %s accepted: %d %s", host, method, name, out.Code, out.Body.String())
				}
			}
		}
	}
}
func TestServeRequestRequiresTailnetHostAndOwner(t *testing.T) {
	h := gateServer(t, "node.tailnet.ts.net", "owner@example.com", "")
	serve := func(login string) map[string]string {
		header := map[string]string{
			"X-Forwarded-For":   "100.64.0.2",
			"X-Forwarded-Host":  "node.tailnet.ts.net",
			"X-Forwarded-Proto": "https",
		}
		if login != "" {
			header["Tailscale-User-Login"] = login
		}
		return header
	}
	if out := serveGate(h, http.MethodGet, "node.tailnet.ts.net", serve("Owner@Example.com")); out.Code != 200 {
		t.Fatalf("genuine Serve request rejected: %d %s", out.Code, out.Body.String())
	}
	// Serve adds no identity headers for tagged source nodes.
	if out := serveGate(h, http.MethodGet, "node.tailnet.ts.net", serve("")); out.Code != 403 || problemCode(t, out) != "TAILNET_IDENTITY_REJECTED" {
		t.Fatalf("tagged node accepted: %d %s", out.Code, out.Body.String())
	}
	// A genuine Serve request carries the tailnet Host, never a loopback one.
	if out := serveGate(h, http.MethodGet, "127.0.0.1:8787", serve("owner@example.com")); out.Code != 403 || problemCode(t, out) != "HOST_REJECTED" {
		t.Fatalf("loopback Host with owner login accepted: %d %s", out.Code, out.Body.String())
	}
	// Local browser and Vite dev proxy requests carry no Serve headers.
	for _, host := range []string{"", "node.tailnet.ts.net"} {
		local := gateServer(t, host, "owner@example.com", "")
		if out := serveGate(local, http.MethodGet, "127.0.0.1:8787", map[string]string{"Origin": "http://127.0.0.1:5173"}); out.Code != 200 {
			t.Fatalf("tailnet host %q: local request rejected: %d %s", host, out.Code, out.Body.String())
		}
	}
}
func TestResponsesCarrySecurityHeadersAndStaticHasNoListing(t *testing.T) {
	dir := t.TempDir()
	if e := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); e != nil {
		t.Fatal(e)
	}
	if e := os.MkdirAll(filepath.Join(dir, "empty"), 0o755); e != nil {
		t.Fatal(e)
	}
	for name, body := range map[string]string{"index.html": "<!doctype html>", "assets/app.js": "export {}"} {
		if e := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); e != nil {
			t.Fatal(e)
		}
	}
	h := gateServer(t, "", "", dir)
	for _, tc := range []struct {
		path string
		want int
	}{
		{"/", 200},
		{"/assets/app.js", 200},
		{"/assets/", 404},
		{"/assets", 404},
		{"/empty/", 404},
		{"/missing", 404},
		{"/api/sessions", 200},
	} {
		req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8787"+tc.path, nil)
		out := httptest.NewRecorder()
		h.ServeHTTP(out, req)
		if out.Code != tc.want {
			t.Fatalf("%s: %d want %d body %q", tc.path, out.Code, tc.want, out.Body.String())
		}
		if tc.path == "/" && !strings.Contains(out.Body.String(), "<!doctype html>") {
			t.Fatalf("index not served: %q", out.Body.String())
		}
		assertSecurityHeaders(t, tc.path, out.Header())
	}
	rejected := serveGate(h, http.MethodGet, "127.0.0.1:8787", map[string]string{"X-Forwarded-For": "100.64.0.2"})
	if rejected.Code != 403 {
		t.Fatalf("proxied request accepted: %d", rejected.Code)
	}
	assertSecurityHeaders(t, "rejected", rejected.Header())
}
func assertSecurityHeaders(t *testing.T, label string, header http.Header) {
	t.Helper()
	for name, want := range map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
		"Content-Security-Policy": "frame-ancestors 'none'",
	} {
		if got := header.Get(name); got != want {
			t.Fatalf("%s: %s = %q", label, name, got)
		}
	}
}
