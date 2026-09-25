package herdr

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func socketServer(t *testing.T, serve func(net.Conn)) string {
	t.Helper()
	// Darwin limits sockaddr_un paths to 104 bytes; test names are too long.
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
		c, err := l.Accept()
		if err == nil {
			defer c.Close()
			serve(c)
		}
	}()
	return path
}

func respond(t *testing.T, c net.Conn, method string, result string) {
	t.Helper()
	var req struct {
		ID     string          `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if err := json.NewDecoder(c).Decode(&req); err != nil {
		t.Error(err)
		return
	}
	if req.Method != method {
		t.Errorf("method = %s, want %s", req.Method, method)
	}
	if req.ID == "" {
		t.Error("missing correlation ID")
	}
	if err := json.NewEncoder(c).Encode(map[string]any{"id": req.ID, "result": json.RawMessage(result)}); err != nil {
		t.Error(err)
	}
}

func TestSnapshotPreservesNativeIdentity(t *testing.T) {
	path := socketServer(t, func(c net.Conn) {
		respond(t, c, "session.snapshot", `{"type":"session_snapshot","snapshot":{"version":"0.9.1","protocol":22,"agents":[{"terminal_id":"term_a","pane_id":"w1:p2","tab_id":"w1:t1","workspace_id":"w1","agent":"claude","agent_status":"idle","agent_session":{"source":"herdr:claude","agent":"claude","kind":"id","value":"native-a"}}]}}`)
	})
	s, err := NewGateway(path).Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Agents) != 1 || s.Agents[0].Session == nil || s.Agents[0].Session.Value != "native-a" {
		t.Fatalf("lost identity: %+v", s)
	}
}

func TestMissingAssociationRemainsUnknown(t *testing.T) {
	path := socketServer(t, func(c net.Conn) {
		respond(t, c, "session.snapshot", `{"type":"session_snapshot","snapshot":{"version":"0.9.1","protocol":22,"agents":[{"terminal_id":"term_a","pane_id":"w1:p2","agent":"claude"}]}}`)
	})
	s, err := NewGateway(path).Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s.Agents[0].Session != nil {
		t.Fatal("invented native identity")
	}
}

func TestProcessInfoUsesExplicitPane(t *testing.T) {
	path := socketServer(t, func(c net.Conn) {
		var req struct {
			ID     string `json:"id"`
			Method string `json:"method"`
			Params struct {
				PaneID string `json:"pane_id"`
			} `json:"params"`
		}
		if err := json.NewDecoder(c).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		if req.Method != "pane.process_info" || req.Params.PaneID != "w1:p2" {
			t.Errorf("incorrect target: %+v", req)
		}
		json.NewEncoder(c).Encode(map[string]any{"id": req.ID, "result": json.RawMessage(`{"type":"pane_process_info","process_info":{"pane_id":"w1:p2","shell_pid":10,"foreground_process_group_id":20,"foreground_processes":[{"pid":20,"name":"claude","argv0":"claude"}]}}`)})
	})
	p, err := NewGateway(path).ProcessInfo(context.Background(), "w1:p2")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.ForegroundProcesses) != 1 || p.ForegroundProcesses[0].PID != 20 {
		t.Fatalf("process missing: %+v", p)
	}
}

func TestRejectsEmptyPaneWithoutConnecting(t *testing.T) {
	_, err := NewGateway("/nonexistent").ProcessInfo(context.Background(), "")
	if !errors.Is(err, ErrEmptyTarget) {
		t.Fatalf("got %v", err)
	}
}

func TestRejectsMismatchedResponseTarget(t *testing.T) {
	path := socketServer(t, func(c net.Conn) {
		respond(t, c, "pane.process_info", `{"type":"pane_process_info","process_info":{"pane_id":"w1:p9"}}`)
	})
	_, err := NewGateway(path).ProcessInfo(context.Background(), "w1:p2")
	if err == nil {
		t.Fatal("accepted wrong pane")
	}
}

func TestRejectsInvalidResponse(t *testing.T) {
	for name, wire := range map[string]string{
		"wrong_id":       `{"id":"other","result":{"type":"session_snapshot","snapshot":{}}}`,
		"malformed":      `{`,
		"missing_result": `{"id":"ID"}`,
		"wrong_type":     `{"id":"ID","result":{"type":"agent_info"}}`,
		"null_snapshot":  `{"id":"ID","result":{"type":"session_snapshot","snapshot":null}}`,
		"oversized":      strings.Repeat("x", maxResponseBytes+1),
	} {
		t.Run(name, func(t *testing.T) {
			path := socketServer(t, func(c net.Conn) {
				var req struct {
					ID string `json:"id"`
				}
				json.NewDecoder(c).Decode(&req)
				c.Write([]byte(strings.ReplaceAll(wire, "ID", req.ID) + "\n"))
			})
			_, err := NewGateway(path).Snapshot(context.Background())
			if err == nil {
				t.Fatal("invalid response accepted")
			}
		})
	}
}

func TestAPIErrorsAreTyped(t *testing.T) {
	path := socketServer(t, func(c net.Conn) {
		var req struct {
			ID string `json:"id"`
		}
		json.NewDecoder(c).Decode(&req)
		json.NewEncoder(c).Encode(map[string]any{"id": req.ID, "error": map[string]string{"code": "pane_not_found", "message": "gone"}})
	})
	_, err := NewGateway(path).ProcessInfo(context.Background(), "w1:p2")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "pane_not_found" {
		t.Fatalf("got %v", err)
	}
}

func TestCancellationClosesIdleSocket(t *testing.T) {
	closed := make(chan struct{})
	path := socketServer(t, func(c net.Conn) {
		bufio.NewReader(c).ReadBytes('\n')
		var b [1]byte
		c.Read(b[:])
		close(closed)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := NewGateway(path).Snapshot(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v", err)
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("socket remained open")
	}
}

func TestTerminalSnapshotIsPassiveVisibleANSI(t *testing.T) {
	path := socketServer(t, func(c net.Conn) {
		var req struct {
			ID     string `json:"id"`
			Method string `json:"method"`
			Params struct {
				PaneID    string `json:"pane_id"`
				Source    string `json:"source"`
				Format    string `json:"format"`
				StripANSI bool   `json:"strip_ansi"`
				Lines     *int   `json:"lines"`
			} `json:"params"`
		}
		if err := json.NewDecoder(c).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		if req.Method != "pane.read" || req.Params.PaneID != "w1:p2" || req.Params.Source != "visible" || req.Params.Format != "ansi" || req.Params.StripANSI || req.Params.Lines != nil {
			t.Errorf("unsafe read: %+v", req)
		}
		json.NewEncoder(c).Encode(map[string]any{"id": req.ID, "result": map[string]any{"type": "pane_read", "read": map[string]any{"pane_id": "w1:p2", "source": "visible", "format": "ansi", "text": "\x1b[32mReady\x1b[0m\r\n", "truncated": false}}})
	})
	got, err := NewGateway(path).TerminalSnapshot(context.Background(), "w1:p2")
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "\x1b[32mReady\x1b[0m\r\n" {
		t.Fatalf("lost ANSI: %q", got.Text)
	}
}

func TestTerminalSnapshotRejectsWrongSourceOrTarget(t *testing.T) {
	for _, wire := range []string{
		`{"type":"pane_read","read":{"pane_id":"w1:p9","source":"visible","format":"ansi"}}`,
		`{"type":"pane_read","read":{"pane_id":"w1:p2","source":"recent","format":"ansi"}}`,
		`{"type":"pane_read","read":{"pane_id":"w1:p2","source":"visible","format":"text"}}`,
		`{"type":"pane_read","read":null}`,
	} {
		path := socketServer(t, func(c net.Conn) { respond(t, c, "pane.read", wire) })
		if _, err := NewGateway(path).TerminalSnapshot(context.Background(), "w1:p2"); err == nil {
			t.Fatal("invalid terminal snapshot accepted")
		}
	}
}

func TestPaneSizeUsesLayoutRectOrZoomedArea(t *testing.T) {
	for _, tc := range []struct {
		name string
		wire string
		want PaneSize
	}{
		{"split", `{"type":"pane_layout","layout":{"zoomed":false,"area":{"x":0,"y":0,"width":200,"height":50},"focused_pane_id":"w1:p1","panes":[{"pane_id":"w1:p1","focused":true,"rect":{"x":0,"y":0,"width":100,"height":50}},{"pane_id":"w1:p2","focused":false,"rect":{"x":100,"y":0,"width":100,"height":50}}]}}`, PaneSize{Cols: 100, Rows: 50}},
		{"zoomed", `{"type":"pane_layout","layout":{"zoomed":true,"area":{"x":0,"y":0,"width":161,"height":45},"focused_pane_id":"w1:p2","panes":[{"pane_id":"w1:p2","focused":true,"rect":{"x":80,"y":0,"width":81,"height":45}}]}}`, PaneSize{Cols: 161, Rows: 45}},
	} {
		path := socketServer(t, func(c net.Conn) { respond(t, c, "pane.layout", tc.wire) })
		got, err := NewGateway(path).PaneSize(context.Background(), "w1:p2")
		if err != nil || got != tc.want {
			t.Fatalf("%s: got %+v %v, want %+v", tc.name, got, err, tc.want)
		}
	}
}

func TestPaneSizeRejectsMissingPaneOrEmptyRect(t *testing.T) {
	for _, wire := range []string{
		`{"type":"pane_layout","layout":{"zoomed":false,"area":{"width":200,"height":50},"focused_pane_id":"w1:p1","panes":[{"pane_id":"w1:p1","rect":{"width":200,"height":50}}]}}`,
		`{"type":"pane_layout","layout":{"zoomed":false,"area":{"width":200,"height":50},"focused_pane_id":"w1:p2","panes":[{"pane_id":"w1:p2","rect":{"width":0,"height":50}}]}}`,
		`{"type":"pane_layout","layout":null}`,
	} {
		path := socketServer(t, func(c net.Conn) { respond(t, c, "pane.layout", wire) })
		if _, err := NewGateway(path).PaneSize(context.Background(), "w1:p2"); err == nil {
			t.Fatalf("invalid layout accepted: %s", wire)
		}
	}
}

func TestBindingUsesNativeSessionAndRejectsWrongPane(t *testing.T) {
	path := socketServer(t, func(c net.Conn) {
		var req struct {
			ID     string `json:"id"`
			Method string `json:"method"`
			Params struct {
				Target string `json:"target"`
			} `json:"params"`
		}
		json.NewDecoder(c).Decode(&req)
		if req.Method != "agent.binding" || req.Params.Target != "w1:p2" {
			t.Errorf("wrong lookup %+v", req)
		}
		json.NewEncoder(c).Encode(map[string]any{"id": req.ID, "result": map[string]any{"type": "agent_binding", "binding": map[string]any{"token": "verified", "terminal_id": "term_a", "native_session_id": "native-a", "process_id": 42, "agent": "claude", "cwd": "repo"}}})
	})
	b, e := NewGateway(path).Binding(context.Background(), "w1:p2")
	if e != nil || b.NativeSessionID != "native-a" || b.Token != "verified" {
		t.Fatalf("got %+v %v", b, e)
	}
}
func TestBoundInputUsesSeparateMethodWithoutRetry(t *testing.T) {
	path := socketServer(t, func(c net.Conn) {
		var req struct {
			ID     string `json:"id"`
			Method string `json:"method"`
			Params struct {
				Target  string `json:"target"`
				Binding string `json:"binding"`
				Input   struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"input"`
			} `json:"params"`
		}
		json.NewDecoder(c).Decode(&req)
		if req.Method != "agent.bound_input" || req.Params.Target != "w1:p2" || req.Params.Binding != "verified" || req.Params.Input.Type != "prompt" || req.Params.Input.Text != "hello" {
			t.Errorf("wrong command %+v", req)
		}
		json.NewEncoder(c).Encode(map[string]any{"id": req.ID, "result": map[string]string{"type": "ok"}})
	})
	g := NewGateway(path)
	e := g.BoundInput(context.Background(), "w1:p2", "verified", "prompt", "hello")
	if e != nil {
		t.Fatal(e)
	}
}

// Observed shape of a Herdr 0.9.1 unknown-method rejection. The expected
// variant list is truncated; detection must not depend on it.
const unknownMethodResponse = `{"id":"","error":{"code":"invalid_request","message":"invalid request: unknown variant ` + "`%s`, expected one of `ping`, `session.snapshot`, `pane.read`" + ` at line 1 column 71"}}`

func rejectAs(t *testing.T, method, response string) string {
	return socketServer(t, func(c net.Conn) {
		var req struct {
			Method string `json:"method"`
		}
		if err := json.NewDecoder(c).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		if req.Method != method {
			t.Errorf("method = %s, want %s", req.Method, method)
		}
		c.Write([]byte(response + "\n"))
	})
}

func TestUnknownMethodIsUnsupported(t *testing.T) {
	path := rejectAs(t, "agent.binding", fmt.Sprintf(unknownMethodResponse, "agent.binding"))
	_, err := NewGateway(path).Binding(context.Background(), "w1:p2")
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("got %v", err)
	}
	var api *APIError
	if errors.As(err, &api) {
		t.Fatal("unsupported must not look like a method-level API error")
	}
}

func TestOtherUncorrelatedErrorsStayMismatch(t *testing.T) {
	unsupported := fmt.Sprintf(unknownMethodResponse, "agent.binding")
	for name, response := range map[string]string{
		"other method":  fmt.Sprintf(unknownMethodResponse, "agent.other"),
		"params error":  `{"id":"","error":{"code":"invalid_request","message":"invalid request: missing field ` + "`target`" + ` at line 1 column 40"}}`,
		"other code":    strings.Replace(unsupported, "invalid_request", "internal_error", 1),
		"mismatched id": strings.Replace(unsupported, `"id":""`, `"id":"other"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			path := rejectAs(t, "agent.binding", response)
			_, err := NewGateway(path).Binding(context.Background(), "w1:p2")
			if err == nil || errors.Is(err, ErrUnsupported) || err.Error() != "Herdr response correlation mismatch" {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestObserveConditionalInput(t *testing.T) {
	for _, tc := range []struct {
		current string
		err     error
		want    string
	}{
		{ConditionalInputUnknown, errors.New("dial failed"), ConditionalInputUnknown},
		{ConditionalInputUnknown, nil, ConditionalInputSupported},
		{ConditionalInputUnknown, &APIError{Code: "session_ended"}, ConditionalInputSupported},
		{ConditionalInputSupported, fmt.Errorf("%w: agent.binding", ErrUnsupported), ConditionalInputUnsupported},
		{ConditionalInputUnsupported, nil, ConditionalInputUnsupported},
	} {
		if got := ObserveConditionalInput(tc.current, tc.err); got != tc.want {
			t.Fatalf("Observe(%s, %v) = %s, want %s", tc.current, tc.err, got, tc.want)
		}
	}
}
