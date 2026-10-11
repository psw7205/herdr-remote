package httpapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/psw7205/herdr-remote/internal/command"
	"github.com/psw7205/herdr-remote/internal/gitstate"
	"github.com/psw7205/herdr-remote/internal/stream"
)

type countingChanges struct{ calls atomic.Int32 }

func (c *countingChanges) Changes(context.Context, string) (gitstate.Changes, error) {
	c.calls.Add(1)
	return gitstate.Changes{Repository: true, Files: []gitstate.File{}}, nil
}
func (c *countingChanges) Diff(context.Context, string, string) (gitstate.Diff, error) {
	c.calls.Add(1)
	return gitstate.Diff{Content: gitstate.ContentText}, nil
}

const allowedOrigin = "http://localhost:5173"

func fetchMetaServer(t *testing.T) (*Server, *fakeSessions, *countingChanges) {
	t.Helper()
	store, err := command.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	static := t.TempDir()
	if err := os.WriteFile(filepath.Join(static, "index.html"), []byte("<!doctype html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	sessions := &fakeSessions{hub: stream.New("claude:native"), project: t.TempDir()}
	changes := &countingChanges{}
	server := New(sessions, store, []string{allowedOrigin}, static)
	server.SetChanges(changes)
	return server, sessions, changes
}

// A no-cors subresource such as <img src> carries Sec-Fetch-Site but no
// Origin; the per-route checks alone would let it start the read.
func TestCrossSiteAPIRequestIsRejectedBeforeAnyWork(t *testing.T) {
	const changes = "/api/sessions/claude:native/changes"
	for _, tc := range []struct {
		name, method, target, site, origin string
		code                               int
		problem                            string
	}{
		{"cross-site changes without Origin", http.MethodGet, changes, "cross-site", "", 403, "CROSS_SITE_REJECTED"},
		{"same-site diff without Origin", http.MethodGet, changes + "/diff?path=a.txt", "same-site", "", 403, "CROSS_SITE_REJECTED"},
		{"cross-site list with a foreign Origin", http.MethodGet, "/api/sessions", "cross-site", "https://evil.example", 403, "CROSS_SITE_REJECTED"},
		{"same-site snapshot with a null Origin", http.MethodGet, "/api/sessions/claude:native", "same-site", "null", 403, "CROSS_SITE_REJECTED"},
		{"cross-site terminal without Origin", http.MethodGet, "/api/sessions/claude:native/terminal", "cross-site", "", 403, "CROSS_SITE_REJECTED"},
		{"cross-site command with a foreign Origin", http.MethodPost, "/api/sessions/claude:native/commands", "cross-site", "https://evil.example", 403, "CROSS_SITE_REJECTED"},
		{"cross-site changes with an allowed Origin", http.MethodGet, changes, "cross-site", allowedOrigin, 200, ""},
		{"same-site changes with an allowed Origin", http.MethodGet, changes, "same-site", allowedOrigin, 200, ""},
		{"same-origin changes", http.MethodGet, changes, "same-origin", "", 200, ""},
		{"user-initiated changes", http.MethodGet, changes, "none", "", 200, ""},
		{"changes without Fetch Metadata", http.MethodGet, changes, "", "", 200, ""},
		{"list without Fetch Metadata and a foreign Origin", http.MethodGet, "/api/sessions", "", "https://evil.example", 200, ""},
		{"same-origin changes with a foreign Origin", http.MethodGet, changes, "same-origin", "https://evil.example", 403, "ORIGIN_REJECTED"},
		{"cross-site static page", http.MethodGet, "/", "cross-site", "", 200, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, sessions, reader := fetchMetaServer(t)
			r := httptest.NewRequest(tc.method, "http://127.0.0.1:8787"+tc.target, strings.NewReader("{}"))
			if tc.method == http.MethodPost {
				r.Header.Set("Content-Type", "application/json")
			}
			if tc.site != "" {
				r.Header.Set("Sec-Fetch-Site", tc.site)
			}
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			out := httptest.NewRecorder()
			server.Handler().ServeHTTP(out, r)
			if out.Code != tc.code {
				t.Fatalf("status %d, want %d: %s", out.Code, tc.code, out.Body.String())
			}
			assertSecurityHeaders(t, tc.name, out.Header())
			if tc.problem == "" {
				return
			}
			if got := problemCode(t, out); got != tc.problem {
				t.Fatalf("error %q, want %q", got, tc.problem)
			}
			if tc.problem == "CROSS_SITE_REJECTED" {
				if n := sessions.calls.Load() + reader.calls.Load(); n != 0 {
					t.Fatalf("rejected request reached the backend %d times", n)
				}
			}
		})
	}
}

func TestCrossSiteWebSocketIsRejected(t *testing.T) {
	server, sessions, _ := fetchMetaServer(t)
	ts := httptest.NewServer(server.Handler())
	t.Cleanup(ts.Close)
	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/sessions/claude:native/events"
	dial := func(site, origin string) (*websocket.Conn, *http.Response, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		return websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: http.Header{"Sec-Fetch-Site": {site}, "Origin": {origin}}})
	}
	for _, origin := range []string{"https://evil.example", "null"} {
		_, res, err := dial("cross-site", origin)
		if err == nil || res == nil || res.StatusCode != 403 {
			t.Fatalf("origin %q: cross-site handshake = %v, %v", origin, res, err)
		}
		body, _ := io.ReadAll(res.Body)
		if !strings.Contains(string(body), "CROSS_SITE_REJECTED") {
			t.Fatalf("origin %q: handshake body %q", origin, body)
		}
	}
	if n := sessions.calls.Load(); n != 0 {
		t.Fatalf("rejected handshake reached the registry %d times", n)
	}
	conn, _, err := dial("same-origin", allowedOrigin)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close(websocket.StatusNormalClosure, "done")
}
