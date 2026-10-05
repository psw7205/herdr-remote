package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/psw7205/herdr-remote/internal/command"
	"github.com/psw7205/herdr-remote/internal/gitstate"
	"github.com/psw7205/herdr-remote/internal/stream"
)

func changesServer(t *testing.T, project string, reader ChangeReader) *Server {
	t.Helper()
	store, err := command.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server := New(&fakeSessions{hub: stream.New("claude:native"), project: project}, store, []string{"http://localhost:5173"}, "")
	server.SetTailnetIdentity("node.tailnet.ts.net", "owner@example.com")
	server.SetChanges(reader)
	return server
}

func gitRepo(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "-q", "-m", "c")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func get(t *testing.T, handler http.Handler, target string, header map[string]string) (int, map[string]any) {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8787"+target, nil)
	r.Host = "127.0.0.1:8787"
	for k, v := range header {
		if k == "Host" {
			r.Host = v
			continue
		}
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	return w.Code, body
}

func TestChangedFilesRoutes(t *testing.T) {
	dir := gitRepo(t)
	bin, err := gitstate.Find()
	if err != nil {
		t.Skip(err)
	}
	handler := changesServer(t, dir, gitstate.New(bin)).Handler()
	diffPath := func(path string) string {
		return "/api/sessions/claude:native/changes/diff?path=" + url.QueryEscape(path)
	}

	code, body := get(t, handler, "/api/sessions/claude:native/changes", nil)
	files, _ := body["files"].([]any)
	if code != 200 || body["repository"] != true || body["branch"] != "main" || len(files) != 1 {
		t.Fatalf("list = %d %v", code, body)
	}
	code, body = get(t, handler, diffPath("a.txt"), map[string]string{"Origin": "http://localhost:5173"})
	if code != 200 || !strings.Contains(body["diff"].(string), "-old\n+new\n") || body["content"] != "text" || body["status"] != "modified" {
		t.Fatalf("diff = %d %v", code, body)
	}
	for _, tc := range []struct {
		target string
		header map[string]string
		code   int
		error  string
	}{
		{"/api/sessions/claude:other/changes", nil, 404, "SESSION_NOT_FOUND"},
		{"/api/sessions/claude:other/changes/diff?path=a.txt", nil, 404, "SESSION_NOT_FOUND"},
		{"/api/sessions/claude:native/changes", map[string]string{"Origin": "https://evil.example"}, 403, "ORIGIN_REJECTED"},
		{diffPath("a.txt"), map[string]string{"Origin": "null"}, 403, "ORIGIN_REJECTED"},
		{"/api/sessions/claude:native/changes", map[string]string{"Host": "evil.example"}, 403, "HOST_REJECTED"},
		{"/api/sessions/claude:native/changes", map[string]string{"Host": "node.tailnet.ts.net", "Tailscale-User-Login": "peer@example.com"}, 403, "TAILNET_IDENTITY_REJECTED"},
		{"/api/sessions/claude:native/changes", map[string]string{"X-Forwarded-For": "100.64.0.9"}, 403, "TAILNET_IDENTITY_REJECTED"},
		{"/api/sessions/claude:native/changes/diff", nil, 400, "INVALID_PATH"},
		{"/api/sessions/claude:native/changes/diff?path=a.txt&path=b.txt", nil, 400, "INVALID_PATH"},
		{diffPath("a.txt\x00"), nil, 400, "INVALID_PATH"},
		{diffPath(strings.Repeat("a", 4097)), nil, 400, "INVALID_PATH"},
		{diffPath("./a.txt"), nil, 404, "FILE_NOT_CHANGED"},
		{diffPath("../a.txt"), nil, 404, "FILE_NOT_CHANGED"},
		{diffPath(filepath.Join(dir, "a.txt")), nil, 404, "FILE_NOT_CHANGED"},
		{diffPath(":(glob)*"), nil, 404, "FILE_NOT_CHANGED"},
	} {
		code, body := get(t, handler, tc.target, tc.header)
		if code != tc.code || body["error"] != tc.error {
			t.Errorf("%s %v = %d %v, want %d %s", tc.target, tc.header, code, body, tc.code, tc.error)
		}
	}
	code, body = get(t, handler, "/api/sessions/claude:native/changes", map[string]string{"Host": "node.tailnet.ts.net", "Tailscale-User-Login": "owner@example.com"})
	if code != 200 || body["repository"] != true {
		t.Fatalf("owner through Serve = %d %v", code, body)
	}
	r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8787/api/sessions/claude:native/changes", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound && w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST changes = %d", w.Code)
	}
}

func TestChangedFilesOutsideARepository(t *testing.T) {
	gitRepo(t)
	bin, err := gitstate.Find()
	if err != nil {
		t.Skip(err)
	}
	plain := t.TempDir()
	handler := changesServer(t, plain, gitstate.New(bin)).Handler()
	code, body := get(t, handler, "/api/sessions/claude:native/changes", nil)
	if code != 200 || body["repository"] != false {
		t.Fatalf("plain = %d %v", code, body)
	}
	if code, body = get(t, handler, "/api/sessions/claude:native/changes/diff?path=a.txt", nil); code != 404 || body["error"] != "NOT_A_REPOSITORY" {
		t.Fatalf("plain diff = %d %v", code, body)
	}
	handler = changesServer(t, filepath.Join(plain, "removed-worktree"), gitstate.New(bin)).Handler()
	if code, body = get(t, handler, "/api/sessions/claude:native/changes", nil); code != 404 || body["error"] != "PROJECT_NOT_FOUND" {
		t.Fatalf("missing project = %d %v", code, body)
	}
	handler = changesServer(t, plain, gitstate.New("")).Handler()
	if code, body = get(t, handler, "/api/sessions/claude:native/changes", nil); code != 503 || body["error"] != "GIT_UNAVAILABLE" {
		t.Fatalf("no git = %d %v", code, body)
	}
	handler = changesServer(t, plain, nil).Handler()
	if code, body = get(t, handler, "/api/sessions/claude:native/changes", nil); code != 404 || body["error"] != "CHANGES_UNAVAILABLE" {
		t.Fatalf("disabled = %d %v", code, body)
	}
}
