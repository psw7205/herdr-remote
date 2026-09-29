package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/psw7205/herdr-remote/internal/command"
	"github.com/psw7205/herdr-remote/internal/herdr"
	"github.com/psw7205/herdr-remote/internal/session"
	"github.com/psw7205/herdr-remote/internal/stream"
)

const testCandidate = "0123456789abcdef0123456789abcdef"

type fakeStarter struct {
	calls        atomic.Int32
	result       session.StartResult
	newWorkspace bool
	candidateErr error
	// release, when set, holds Start until closed.
	release chan struct{}
	ctxErr  atomic.Value
}

func (f *fakeStarter) Kinds() []string    { return []string{"claude"} }
func (f *fakeStarter) NewWorkspace() bool { return f.newWorkspace }
func (f *fakeStarter) Candidates(context.Context) ([]session.Candidate, error) {
	if f.candidateErr != nil {
		return nil, f.candidateErr
	}
	return []session.Candidate{{ID: testCandidate, Name: "api", Root: "work", Placement: session.PlacementNewWorkspace}}, nil
}
func (f *fakeStarter) Start(ctx context.Context, _, candidate, kind, placement string) session.StartResult {
	f.calls.Add(1)
	if f.release != nil {
		<-f.release
		f.ctxErr.Store(fmt.Sprint(ctx.Err()))
	}
	return f.result
}

func newStartServer(t *testing.T, starter *fakeStarter) *httptest.Server {
	t.Helper()
	store, err := command.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	api := New(&fakeSessions{hub: stream.New("claude:native")}, store, []string{"http://localhost:5173"}, "")
	if starter != nil {
		api.SetStarter(starter)
	}
	server := httptest.NewServer(api.Handler())
	t.Cleanup(server.Close)
	return server
}

func startBody(id, kind, placement string) string {
	return fmt.Sprintf(`{"command_id":%q,"command_type":"session_start","payload":{"candidate_id":%q,"kind":%q,"placement":%q}}`, id, testCandidate, kind, placement)
}

func postStart(t *testing.T, ctx context.Context, server *httptest.Server, body string, origin bool) (*http.Response, command.Result, error) {
	t.Helper()
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/api/sessions", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if origin {
		r.Header.Set("Origin", "http://localhost:5173")
	}
	r.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(r)
	if err != nil {
		return nil, command.Result{}, err
	}
	defer res.Body.Close()
	var result command.Result
	_ = json.NewDecoder(res.Body).Decode(&result)
	return res, result, nil
}

func TestStartRetryReturnsSameCreatedTopology(t *testing.T) {
	starter := &fakeStarter{result: session.StartResult{Status: "accepted", Code: "AGENT_NOT_READY", Created: &herdr.Created{WorkspaceID: "w2", TabID: "w2:t1", PaneID: "w2:p1"}, Agent: "mobile-000000000000"}}
	server := newStartServer(t, starter)
	body := startBody("00000000-0000-4000-8000-000000000010", "claude", "new_workspace")
	var first command.Result
	for attempt := range 2 {
		res, result, err := postStart(t, context.Background(), server, body, true)
		if err != nil || res.StatusCode != 202 || result.Status != "accepted" || result.Code != "AGENT_NOT_READY" || result.Created == nil || result.Created.PaneID != "w2:p1" || result.Created.Agent != "mobile-000000000000" {
			t.Fatalf("attempt %d: %v %+v %v", attempt, res.StatusCode, result, err)
		}
		if attempt == 1 && *result.Created != *first.Created {
			t.Fatalf("retry changed result: %+v", result)
		}
		first = result
	}
	if starter.calls.Load() != 1 {
		t.Fatalf("dispatched %d times", starter.calls.Load())
	}
	// The same ID with another placement is a different request.
	res, result, _ := postStart(t, context.Background(), server, startBody("00000000-0000-4000-8000-000000000010", "claude", "new_tab"), true)
	if res.StatusCode != 409 || result.Code != "COMMAND_ID_CONFLICT" || starter.calls.Load() != 1 {
		t.Fatalf("conflict: %d %+v", res.StatusCode, result)
	}
}

func TestStartDeliveryUnknownIsNeverRedispatched(t *testing.T) {
	starter := &fakeStarter{result: session.StartResult{Status: "delivery_unknown", Code: "DELIVERY_UNKNOWN"}}
	server := newStartServer(t, starter)
	body := startBody("00000000-0000-4000-8000-000000000011", "claude", "new_workspace")
	for range 2 {
		res, result, err := postStart(t, context.Background(), server, body, true)
		if err != nil || res.StatusCode != 202 || result.Status != "delivery_unknown" {
			t.Fatalf("%v %+v %v", res.StatusCode, result, err)
		}
	}
	if starter.calls.Load() != 1 {
		t.Fatalf("dispatched %d times", starter.calls.Load())
	}
}

func TestStartRejectedIsConflict(t *testing.T) {
	starter := &fakeStarter{result: session.StartResult{Status: "rejected", Code: "CANDIDATE_CHANGED"}}
	res, result, err := postStart(t, context.Background(), newStartServer(t, starter), startBody("00000000-0000-4000-8000-000000000012", "claude", "new_workspace"), true)
	if err != nil || res.StatusCode != 409 || result.Code != "CANDIDATE_CHANGED" {
		t.Fatalf("%v %+v %v", res.StatusCode, result, err)
	}
}

func TestStartOutlivesAbandonedRequest(t *testing.T) {
	starter := &fakeStarter{release: make(chan struct{}), result: session.StartResult{Status: "accepted", Created: &herdr.Created{WorkspaceID: "w2", TabID: "w2:t1", PaneID: "w2:p1"}}}
	server := newStartServer(t, starter)
	body := startBody("00000000-0000-4000-8000-000000000013", "claude", "new_workspace")
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _, err := postStart(t, ctx, server, body, true)
		if err == nil {
			t.Error("abandoned request completed")
		}
	}()
	for starter.calls.Load() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	wg.Wait()
	// While the first dispatch runs, a retry sees only the uncertain barrier.
	if _, result, _ := postStart(t, context.Background(), server, body, true); result.Status != "delivery_unknown" {
		t.Fatalf("in-flight retry: %+v", result)
	}
	close(starter.release)
	deadline := time.Now().Add(2 * time.Second)
	for {
		_, result, _ := postStart(t, context.Background(), server, body, true)
		if result.Status == "accepted" && result.Created != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("receipt never finished: %+v", result)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := starter.ctxErr.Load(); got != "<nil>" {
		t.Fatalf("dispatch context was cancelled with the request: %v", got)
	}
	if starter.calls.Load() != 1 {
		t.Fatalf("dispatched %d times", starter.calls.Load())
	}
}

func TestStartValidatesRequestBeforeReceipt(t *testing.T) {
	starter := &fakeStarter{result: session.StartResult{Status: "accepted"}}
	server := newStartServer(t, starter)
	id := "00000000-0000-4000-8000-000000000014"
	for name, body := range map[string]string{
		"codex":          startBody(id, "codex", "new_workspace"),
		"placement":      startBody(id, "claude", "split"),
		"type":           strings.Replace(startBody(id, "claude", "new_tab"), "session_start", "prompt", 1),
		"path":           fmt.Sprintf(`{"command_id":%q,"command_type":"session_start","payload":{"candidate_id":"/work/api","kind":"claude","placement":"new_tab"}}`, id),
		"agent_args":     fmt.Sprintf(`{"command_id":%q,"command_type":"session_start","payload":{"candidate_id":%q,"kind":"claude","placement":"new_tab","args":["--resume"]}}`, id, testCandidate),
		"trailing_value": startBody(id, "claude", "new_tab") + `{}`,
	} {
		res, _, err := postStart(t, context.Background(), server, body, true)
		if err != nil || res.StatusCode != 400 {
			t.Fatalf("%s: status %v err %v", name, res.StatusCode, err)
		}
	}
	res, _, _ := postStart(t, context.Background(), server, startBody(id, "claude", "new_tab"), false)
	if res.StatusCode != 403 {
		t.Fatalf("missing Origin: %d", res.StatusCode)
	}
	if starter.calls.Load() != 0 {
		t.Fatal("invalid start dispatched")
	}
	// Rejected requests reserved nothing: the ID is still usable.
	if res, _, _ := postStart(t, context.Background(), server, startBody(id, "claude", "new_tab"), true); res.StatusCode != 202 {
		t.Fatalf("valid start: %d", res.StatusCode)
	}
}

func TestStartRequiresTailnetOwnerIdentity(t *testing.T) {
	store, _ := command.Open(t.TempDir())
	starter := &fakeStarter{result: session.StartResult{Status: "accepted"}}
	api := New(&fakeSessions{hub: stream.New("claude:native")}, store, []string{"https://private.ts.net"}, "")
	api.SetTailnetIdentity("private.ts.net", "owner@example.com")
	api.SetStarter(starter)
	r := httptest.NewRequest(http.MethodPost, "https://private.ts.net/api/sessions", strings.NewReader(startBody("00000000-0000-4000-8000-000000000015", "claude", "new_tab")))
	r.Host = "private.ts.net"
	r.Header.Set("Origin", "https://private.ts.net")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Tailscale-User-Login", "other@example.com")
	w := httptest.NewRecorder()
	api.Handler().ServeHTTP(w, r)
	if w.Code != 403 || starter.calls.Load() != 0 {
		t.Fatalf("status %d calls %d", w.Code, starter.calls.Load())
	}
}

func TestPromptRejectsSessionStartFields(t *testing.T) {
	server, sessions := newTestServer(t)
	body := `{"command_id":"00000000-0000-4000-8000-000000000016","session_id":"claude:native","runtime_binding":"bound","command_type":"prompt","payload":{"text":"hello","kind":"claude"}}`
	r, _ := http.NewRequest(http.MethodPost, server.URL+"/api/sessions/claude:native/commands", strings.NewReader(body))
	r.Header.Set("Origin", "http://localhost:5173")
	r.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 400 || sessions.count.Load() != 0 {
		t.Fatalf("status %d", res.StatusCode)
	}
}

func TestStartCapabilityAndCandidates(t *testing.T) {
	read := func(server *httptest.Server, path string) (int, map[string]json.RawMessage) {
		res, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var body map[string]json.RawMessage
		_ = json.NewDecoder(res.Body).Decode(&body)
		return res.StatusCode, body
	}
	_, body := read(newStartServer(t, nil), "/api/sessions")
	if string(body["start"]) != `{"kinds":[],"new_workspace":false}` {
		t.Fatalf("disabled start = %s", body["start"])
	}
	if status, _ := read(newStartServer(t, nil), "/api/start-candidates"); status != 404 {
		t.Fatalf("candidates without starter: %d", status)
	}
	starter := &fakeStarter{newWorkspace: true}
	server := newStartServer(t, starter)
	if _, body = read(server, "/api/sessions"); string(body["start"]) != `{"kinds":["claude"],"new_workspace":true}` {
		t.Fatalf("start = %s", body["start"])
	}
	status, body := read(server, "/api/start-candidates")
	if status != 200 || !strings.Contains(string(body["candidates"]), `"id":"`+testCandidate+`"`) || strings.Contains(string(body["candidates"]), "path") {
		t.Fatalf("candidates %d %s", status, body["candidates"])
	}
	starter.candidateErr = errors.New("socket closed")
	if status, _ := read(server, "/api/start-candidates"); status != 503 {
		t.Fatalf("herdr down: %d", status)
	}
}
