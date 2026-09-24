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
	"net/http"
	"net/http/httptest"
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
	return nil
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
