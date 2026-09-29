package herdr

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type call struct {
	Method string
	Params map[string]any
}

// fakeHerdr answers one request per connection, like the real socket, and
// records every request in order.
type fakeHerdr struct {
	mu     sync.Mutex
	calls  []call
	answer func(method string, params map[string]any, n int) (string, *APIError)
}

func (f *fakeHerdr) methods() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.calls))
	for i, c := range f.calls {
		out[i] = c.Method
	}
	return out
}

func (f *fakeHerdr) call(i int) call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[i]
}

func serveFake(t *testing.T, f *fakeHerdr) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "hr-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "api.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				var req struct {
					ID     string         `json:"id"`
					Method string         `json:"method"`
					Params map[string]any `json:"params"`
				}
				if json.NewDecoder(c).Decode(&req) != nil {
					return
				}
				f.mu.Lock()
				f.calls = append(f.calls, call{req.Method, req.Params})
				n := 0
				for _, c := range f.calls {
					if c.Method == req.Method {
						n++
					}
				}
				f.mu.Unlock()
				result, apiErr := f.answer(req.Method, req.Params, n)
				if apiErr != nil {
					json.NewEncoder(c).Encode(map[string]any{"id": req.ID, "error": apiErr})
					return
				}
				json.NewEncoder(c).Encode(map[string]any{"id": req.ID, "result": json.RawMessage(result)})
			}()
		}
	}()
	return path
}

func fastStartTiming(t *testing.T) {
	t.Helper()
	wait, poll, retry, step := agentStartWait, agentPollInterval, shellReadyRetry, shellReadyRetryStep
	agentStartWait, agentPollInterval, shellReadyRetry, shellReadyRetryStep = 300*time.Millisecond, 5*time.Millisecond, 200*time.Millisecond, 5*time.Millisecond
	t.Cleanup(func() {
		agentStartWait, agentPollInterval, shellReadyRetry, shellReadyRetryStep = wait, poll, retry, step
	})
}

func TestTopologyReadsWorkspacesAndPaneCwd(t *testing.T) {
	f := &fakeHerdr{answer: func(string, map[string]any, int) (string, *APIError) {
		return `{"type":"session_snapshot","snapshot":{"version":"0.9.1","protocol":22,"workspaces":[{"workspace_id":"w1","number":1,"label":"repo"}],"panes":[{"pane_id":"w1:p1","workspace_id":"w1","tab_id":"w1:t1","cwd":"/work/repo"}],"agents":[]}}`, nil
	}}
	top, err := NewGateway(serveFake(t, f)).Topology(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(top.Workspaces) != 1 || top.Workspaces[0].ID != "w1" || len(top.Panes) != 1 || top.Panes[0].CWD != "/work/repo" {
		t.Fatalf("topology = %+v", top)
	}
	if m := f.methods(); len(m) != 1 || m[0] != "session.snapshot" {
		t.Fatalf("methods = %v", m)
	}
}

func TestTopologyRejectsMissingLists(t *testing.T) {
	f := &fakeHerdr{answer: func(string, map[string]any, int) (string, *APIError) {
		return `{"type":"session_snapshot","snapshot":{"version":"0.9.1","protocol":22,"agents":[]}}`, nil
	}}
	if _, err := NewGateway(serveFake(t, f)).Topology(context.Background()); err == nil {
		t.Fatal("accepted a snapshot without workspaces or panes")
	}
}

func TestWorkspaceCreateSendsCwdWithoutFocusOrEnv(t *testing.T) {
	f := &fakeHerdr{answer: func(string, map[string]any, int) (string, *APIError) {
		return `{"type":"workspace_created","workspace":{"workspace_id":"w2","number":2,"label":"repo"},"tab":{"tab_id":"w2:t1","workspace_id":"w2"},"root_pane":{"pane_id":"w2:p1","workspace_id":"w2","tab_id":"w2:t1","cwd":"/work/repo"}}`, nil
	}}
	created, err := NewGateway(serveFake(t, f)).WorkspaceCreate(context.Background(), "/work/repo")
	if err != nil {
		t.Fatal(err)
	}
	if created != (Created{WorkspaceID: "w2", TabID: "w2:t1", PaneID: "w2:p1"}) {
		t.Fatalf("created = %+v", created)
	}
	p := f.call(0).Params
	if f.call(0).Method != "workspace.create" || p["cwd"] != "/work/repo" || p["focus"] != false || p["env"] != nil || p["label"] != nil {
		t.Fatalf("request = %+v", f.call(0))
	}
}

func TestTabCreateTargetsWorkspaceAndRejectsOtherWorkspace(t *testing.T) {
	answer := `{"type":"tab_created","tab":{"tab_id":"w1:t3","workspace_id":"w1"},"root_pane":{"pane_id":"w1:p7","workspace_id":"w1","tab_id":"w1:t3"}}`
	f := &fakeHerdr{answer: func(string, map[string]any, int) (string, *APIError) { return answer, nil }}
	g := NewGateway(serveFake(t, f))
	created, err := g.TabCreate(context.Background(), "w1", "/work/repo")
	if err != nil || created != (Created{WorkspaceID: "w1", TabID: "w1:t3", PaneID: "w1:p7"}) {
		t.Fatalf("created = %+v, %v", created, err)
	}
	p := f.call(0).Params
	if f.call(0).Method != "tab.create" || p["workspace_id"] != "w1" || p["cwd"] != "/work/repo" || p["focus"] != false || p["env"] != nil {
		t.Fatalf("request = %+v", f.call(0))
	}
	if _, err := g.TabCreate(context.Background(), "w9", "/work/repo"); err == nil {
		t.Fatal("accepted a tab in another workspace")
	}
	if _, err := g.TabCreate(context.Background(), "", "/work/repo"); !errors.Is(err, ErrEmptyTarget) {
		t.Fatalf("empty workspace: %v", err)
	}
}

const startedWire = `{"type":"agent_started","agent":{"terminal_id":"term_a","pane_id":"w1:p7","name":"mobile-a","agent_status":"unknown","launch_pending":true},"argv":["claude"]}`

func agentWire(status string, ready, pending bool) string {
	b, _ := json.Marshal(map[string]any{"type": "agent_info", "agent": map[string]any{"terminal_id": "term_a", "pane_id": "w1:p7", "name": "mobile-a", "agent": "claude", "agent_status": status, "interactive_ready": ready, "launch_pending": pending}})
	return string(b)
}

func TestStartAgentSendsNoArgsAndWaitsForInteractive(t *testing.T) {
	fastStartTiming(t)
	f := &fakeHerdr{answer: func(method string, _ map[string]any, n int) (string, *APIError) {
		if method == "agent.start" {
			return startedWire, nil
		}
		if n < 3 {
			return agentWire("unknown", false, true), nil
		}
		return agentWire("idle", true, false), nil
	}}
	if err := NewGateway(serveFake(t, f)).StartAgent(context.Background(), "mobile-a", "claude", "w1:p7"); err != nil {
		t.Fatal(err)
	}
	p := f.call(0).Params
	if f.call(0).Method != "agent.start" || p["name"] != "mobile-a" || p["kind"] != "claude" || p["pane_id"] != "w1:p7" || p["args"] != nil || p["timeout_ms"] != nil {
		t.Fatalf("request = %+v", f.call(0))
	}
}

func TestStartAgentOutcomes(t *testing.T) {
	fastStartTiming(t)
	for name, tc := range map[string]struct {
		agent string
		want  error
	}{
		"trust_screen":     {agentWire("blocked", false, true), ErrAgentNotReady},
		"exited":           {agentWire("idle", false, false), ErrAgentExited},
		"never_ready":      {agentWire("working", false, true), ErrAgentUnconfirmed},
		"other_terminal":   {`{"type":"agent_info","agent":{"terminal_id":"term_b","pane_id":"w1:p7","name":"mobile-a","agent_status":"idle","interactive_ready":true}}`, ErrAgentUnconfirmed},
		"other_agent_kind": {`{"type":"agent_info","agent":{"terminal_id":"term_a","pane_id":"w1:p7","name":"mobile-a","agent":"codex","agent_status":"idle","interactive_ready":true}}`, ErrAgentUnconfirmed},
	} {
		t.Run(name, func(t *testing.T) {
			f := &fakeHerdr{answer: func(method string, _ map[string]any, _ int) (string, *APIError) {
				if method == "agent.start" {
					return startedWire, nil
				}
				return tc.agent, nil
			}}
			err := NewGateway(serveFake(t, f)).StartAgent(context.Background(), "mobile-a", "claude", "w1:p7")
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestStartAgentRetriesBusyOnlyWhileShellInitializes(t *testing.T) {
	fastStartTiming(t)
	busy := &APIError{Code: "agent_pane_busy", Message: "busy"}
	initializing := `{"type":"pane_process_info","process_info":{"pane_id":"w1:p7","shell_pid":10,"foreground_process_group_id":10,"foreground_processes":[{"pid":10,"name":"zsh"}]}}`
	f := &fakeHerdr{answer: func(method string, _ map[string]any, n int) (string, *APIError) {
		switch method {
		case "agent.start":
			if n == 1 {
				return "", busy
			}
			return startedWire, nil
		case "pane.process_info":
			return initializing, nil
		}
		return agentWire("idle", true, false), nil
	}}
	if err := NewGateway(serveFake(t, f)).StartAgent(context.Background(), "mobile-a", "claude", "w1:p7"); err != nil {
		t.Fatal(err)
	}
	m := f.methods()
	if len(m) < 3 || m[0] != "agent.start" || m[1] != "pane.process_info" || m[2] != "agent.start" {
		t.Fatalf("methods = %v", m)
	}

	// Another foreground program owns the pane: busy is final.
	other := &fakeHerdr{answer: func(method string, _ map[string]any, _ int) (string, *APIError) {
		if method == "pane.process_info" {
			return `{"type":"pane_process_info","process_info":{"pane_id":"w1:p7","shell_pid":10,"foreground_process_group_id":20,"foreground_processes":[{"pid":20,"name":"vim"}]}}`, nil
		}
		return "", busy
	}}
	err := NewGateway(serveFake(t, other)).StartAgent(context.Background(), "mobile-a", "claude", "w1:p7")
	var api *APIError
	if !errors.As(err, &api) || api.Code != "agent_pane_busy" {
		t.Fatalf("err = %v", err)
	}
	if n := len(other.methods()); n != 2 {
		t.Fatalf("retried a busy pane with another foreground program: %v", other.methods())
	}
}

func TestStartAgentReturnsDefiniteStartRejection(t *testing.T) {
	fastStartTiming(t)
	f := &fakeHerdr{answer: func(string, map[string]any, int) (string, *APIError) {
		return "", &APIError{Code: "agent_name_taken", Message: "taken"}
	}}
	err := NewGateway(serveFake(t, f)).StartAgent(context.Background(), "mobile-a", "claude", "w1:p7")
	var api *APIError
	if !errors.As(err, &api) || api.Code != "agent_name_taken" {
		t.Fatalf("err = %v", err)
	}
	if m := f.methods(); len(m) != 1 {
		t.Fatalf("methods = %v", m)
	}
}
