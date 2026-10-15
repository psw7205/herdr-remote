package binding

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/psw7205/herdr-remote/internal/herdr"
)

func u32(v uint32) *uint32 { return &v }

type sent struct {
	Method string
	Text   string
	Keys   []string
}

type fakeHerdr struct {
	mu        sync.Mutex
	calls     []string
	sends     []sent
	terminal  string
	agentErr  error
	info      herdr.ProcessInfo
	infoErr   error
	nextInfo  []herdr.ProcessInfo
	sendErr   map[string]error
	afterSend func(method string)
}

func (f *fakeHerdr) record(method string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, method)
}

func (f *fakeHerdr) AgentInfo(_ context.Context, paneID string) (herdr.Agent, error) {
	f.record("agent.get")
	if f.agentErr != nil {
		return herdr.Agent{}, f.agentErr
	}
	return herdr.Agent{PaneID: paneID, TerminalID: f.terminal, Agent: "claude", Status: "idle"}, nil
}

func (f *fakeHerdr) ProcessInfo(_ context.Context, paneID string) (herdr.ProcessInfo, error) {
	f.record("pane.process_info")
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.infoErr != nil {
		return herdr.ProcessInfo{}, f.infoErr
	}
	if len(f.nextInfo) > 0 {
		f.info, f.nextInfo = f.nextInfo[0], f.nextInfo[1:]
	}
	info := f.info
	info.PaneID = paneID
	return info, nil
}

func (f *fakeHerdr) send(s sent) error {
	f.record(s.Method)
	f.mu.Lock()
	f.sends = append(f.sends, s)
	err := f.sendErr[s.Method]
	after := f.afterSend
	f.mu.Unlock()
	if after != nil {
		after(s.Method)
	}
	return err
}

func (f *fakeHerdr) SendInput(_ context.Context, _ string, text string, keys []string) error {
	return f.send(sent{"pane.send_input", text, keys})
}
func (f *fakeHerdr) SendKeys(_ context.Context, _ string, keys []string) error {
	return f.send(sent{"pane.send_keys", "", keys})
}
func (f *fakeHerdr) SendText(_ context.Context, _ string, text string) error {
	return f.send(sent{"pane.send_text", text, nil})
}

func (f *fakeHerdr) sent() []sent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.sends)
}

// clock answers successive start time lookups; the last answer repeats.
type clock struct {
	mu      sync.Mutex
	answers []StartTime
	err     error
}

func (c *clock) start(uint32) (StartTime, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return StartTime{}, c.err
	}
	t := c.answers[0]
	if len(c.answers) > 1 {
		c.answers = c.answers[1:]
	}
	return t, nil
}

const (
	leaderPID = 4242
	nativeID  = "00000000-0000-4000-8000-0000000000aa"
)

var started = StartTime{Sec: time.Date(2026, 10, 15, 8, 36, 5, 0, time.UTC).Unix(), Usec: 123456}

func claudeInfo() herdr.ProcessInfo {
	return herdr.ProcessInfo{ShellPID: u32(10), ForegroundProcessGroupID: u32(leaderPID), ForegroundProcesses: []herdr.Process{
		{PID: leaderPID, Name: "2.1.300", Argv0: "claude"},
		{PID: leaderPID + 1, Name: "caffeinate", Argv0: "caffeinate"},
	}}
}

func validMetadata() map[string]any {
	return map[string]any{
		"pid":       leaderPID,
		"sessionId": nativeID,
		"cwd":       "/work/sample",
		"startedAt": 1792053365123,
		"procStart": "Thu Oct 15 08:36:05 2026",
		"kind":      "interactive",
		"status":    "idle",
		"version":   "2.1.300",
		"pidDomain": "sample",
	}
}

func writeMetadata(t *testing.T, root string, pid int, value any, mtime time.Time) {
	t.Helper()
	data, ok := value.([]byte)
	if !ok {
		var err error
		if data, err = json.Marshal(value); err != nil {
			t.Fatal(err)
		}
	}
	dir := filepath.Join(root, "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(dir, fmt.Sprintf("%d.json", pid))
	if err := os.WriteFile(name, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(name, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

func startInstant(s StartTime) time.Time { return time.Unix(s.Sec, s.Usec*1000) }

type fixture struct {
	g     *Gateway
	f     *fakeHerdr
	clock *clock
	root  string
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	root := t.TempDir()
	writeMetadata(t, root, leaderPID, validMetadata(), startInstant(started).Add(2*time.Second))
	f := &fakeHerdr{terminal: "term_a", info: claudeInfo(), sendErr: map[string]error{}}
	c := &clock{answers: []StartTime{started}}
	return fixture{g: &Gateway{api: f, root: root, startTime: c.start}, f: f, clock: c, root: root}
}

func fastSettle(t *testing.T) {
	t.Helper()
	old := promptSettle
	promptSettle = 5 * time.Millisecond
	t.Cleanup(func() { promptSettle = old })
}

func TestBindingVerifiesClaudeLeaderMetadata(t *testing.T) {
	x := newFixture(t)
	b, err := x.g.Binding(context.Background(), "w1:p2")
	if err != nil {
		t.Fatal(err)
	}
	want := herdr.Binding{Token: token("term_a", leaderPID, started, nativeID), TerminalID: "term_a", NativeSessionID: nativeID, ProcessID: leaderPID, Agent: "claude", CWD: "/work/sample"}
	if b != want {
		t.Fatalf("got %+v, want %+v", b, want)
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(b.Token) {
		t.Fatalf("token %q is not lowercase hex sha256", b.Token)
	}
	if got := strings.Join(x.f.calls, ","); got != "agent.get,pane.process_info,pane.process_info" {
		t.Fatalf("calls %s", got)
	}
}

func TestBindingRequiresClaudeForegroundLeader(t *testing.T) {
	for name, info := range map[string]herdr.ProcessInfo{
		"no foreground group": {ShellPID: u32(10), ForegroundProcesses: claudeInfo().ForegroundProcesses},
		"no shell":            {ForegroundProcessGroupID: u32(leaderPID), ForegroundProcesses: claudeInfo().ForegroundProcesses},
		"leader missing":      {ShellPID: u32(10), ForegroundProcessGroupID: u32(leaderPID), ForegroundProcesses: []herdr.Process{{PID: leaderPID + 1, Name: "claude", Argv0: "claude"}}},
		"shell in foreground": {ShellPID: u32(10), ForegroundProcessGroupID: u32(10), ForegroundProcesses: []herdr.Process{{PID: 10, Name: "zsh", Argv0: "-zsh"}, {PID: leaderPID, Name: "2.1.300", Argv0: "claude"}}},
		"wrapped by node":     {ShellPID: u32(10), ForegroundProcessGroupID: u32(leaderPID), ForegroundProcesses: []herdr.Process{{PID: leaderPID, Name: "claude", Argv0: "node"}}},
		"other agent":         {ShellPID: u32(10), ForegroundProcessGroupID: u32(leaderPID), ForegroundProcesses: []herdr.Process{{PID: leaderPID, Name: "codex", Argv0: "codex"}}},
	} {
		t.Run(name, func(t *testing.T) {
			x := newFixture(t)
			x.f.info = info
			if _, err := x.g.Binding(context.Background(), "w1:p2"); !errors.Is(err, ErrUnverified) {
				t.Fatalf("got %v", err)
			}
		})
	}
	for name, p := range map[string]herdr.Process{
		"argv0 path":        {PID: leaderPID, Name: "2.1.300", Argv0: "/Users/sample/.local/bin/claude"},
		"name without argv": {PID: leaderPID, Name: "claude"},
	} {
		t.Run(name, func(t *testing.T) {
			x := newFixture(t)
			x.f.info.ForegroundProcesses = []herdr.Process{p}
			if _, err := x.g.Binding(context.Background(), "w1:p2"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestBindingRejectsMetadataOfAnotherIncarnation(t *testing.T) {
	cases := map[string]func(m map[string]any){
		"pid mismatch":      func(m map[string]any) { m["pid"] = leaderPID + 1 },
		"procStart off 1s":  func(m map[string]any) { m["procStart"] = "Thu Oct 15 08:36:06 2026" },
		"procStart local":   func(m map[string]any) { m["procStart"] = "Thu Oct 15 17:36:05 2026" },
		"kind not interact": func(m map[string]any) { m["kind"] = "print" },
		"kind missing":      func(m map[string]any) { delete(m, "kind") },
		"empty session":     func(m map[string]any) { m["sessionId"] = "" },
		"empty cwd":         func(m map[string]any) { m["cwd"] = "" },
		"pid as string":     func(m map[string]any) { m["pid"] = fmt.Sprint(leaderPID) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			x := newFixture(t)
			m := validMetadata()
			mutate(m)
			writeMetadata(t, x.root, leaderPID, m, startInstant(started).Add(time.Second))
			if _, err := x.g.Binding(context.Background(), "w1:p2"); !errors.Is(err, ErrUnverified) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestBindingMetadataMtimeAndSize(t *testing.T) {
	x := newFixture(t)
	ctx := context.Background()
	writeMetadata(t, x.root, leaderPID, validMetadata(), startInstant(started).Add(-time.Microsecond))
	if _, err := x.g.Binding(ctx, "w1:p2"); !errors.Is(err, ErrUnverified) {
		t.Fatalf("metadata older than the process accepted: %v", err)
	}
	writeMetadata(t, x.root, leaderPID, validMetadata(), startInstant(started))
	if _, err := x.g.Binding(ctx, "w1:p2"); err != nil {
		t.Fatalf("metadata written at process start rejected: %v", err)
	}
	padded := func(size int) []byte {
		m := validMetadata()
		base, _ := json.Marshal(m)
		m["pad"] = strings.Repeat("x", size-len(base)-len(`,"pad":""`))
		data, _ := json.Marshal(m)
		if len(data) != size {
			t.Fatalf("padded to %d, want %d", len(data), size)
		}
		return data
	}
	writeMetadata(t, x.root, leaderPID, padded(maxMetadataBytes), startInstant(started).Add(time.Second))
	if _, err := x.g.Binding(ctx, "w1:p2"); err != nil {
		t.Fatalf("metadata at the size cap rejected: %v", err)
	}
	writeMetadata(t, x.root, leaderPID, padded(maxMetadataBytes+1), startInstant(started).Add(time.Second))
	if _, err := x.g.Binding(ctx, "w1:p2"); !errors.Is(err, ErrUnverified) {
		t.Fatalf("oversized metadata accepted: %v", err)
	}
}

func TestBindingRejectsMissingOrIrregularMetadata(t *testing.T) {
	x := newFixture(t)
	name := filepath.Join(x.root, "sessions", fmt.Sprintf("%d.json", leaderPID))
	if err := os.Remove(name); err != nil {
		t.Fatal(err)
	}
	if _, err := x.g.Binding(context.Background(), "w1:p2"); !errors.Is(err, ErrUnverified) {
		t.Fatalf("missing metadata: %v", err)
	}
	if err := os.Mkdir(name, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := x.g.Binding(context.Background(), "w1:p2"); !errors.Is(err, ErrUnverified) {
		t.Fatalf("directory accepted: %v", err)
	}
}

func TestBindingRechecksAfterFileIO(t *testing.T) {
	moved := claudeInfo()
	moved.ForegroundProcessGroupID = u32(10)
	otherShell := claudeInfo()
	otherShell.ShellPID = u32(11)
	for name, setup := range map[string]func(x fixture){
		"start time changed":        func(x fixture) { x.clock.answers = []StartTime{started, {Sec: started.Sec, Usec: started.Usec + 1}} },
		"start time lookup failed":  func(x fixture) { x.clock.answers = []StartTime{started, {}} },
		"foreground changed":        func(x fixture) { x.f.nextInfo = []herdr.ProcessInfo{claudeInfo(), moved} },
		"foreground group vanished": func(x fixture) { x.f.nextInfo = []herdr.ProcessInfo{claudeInfo(), {ShellPID: u32(10)}} },
		"shell changed":             func(x fixture) { x.f.nextInfo = []herdr.ProcessInfo{claudeInfo(), otherShell} },
	} {
		t.Run(name, func(t *testing.T) {
			x := newFixture(t)
			setup(x)
			if _, err := x.g.Binding(context.Background(), "w1:p2"); !errors.Is(err, ErrUnverified) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestBindingFailsClosedWithoutStartTime(t *testing.T) {
	x := newFixture(t)
	x.clock.err = errStartTimeUnsupported
	if _, err := x.g.Binding(context.Background(), "w1:p2"); !errors.Is(err, ErrUnverified) {
		t.Fatalf("got %v", err)
	}
}

func TestBindingReturnsHerdrErrors(t *testing.T) {
	x := newFixture(t)
	x.f.infoErr = fmt.Errorf("%w: pane.process_info", herdr.ErrUnsupported)
	if _, err := x.g.Binding(context.Background(), "w1:p2"); !errors.Is(err, herdr.ErrUnsupported) || errors.Is(err, ErrUnverified) {
		t.Fatalf("got %v", err)
	}
	if _, err := x.g.Binding(context.Background(), ""); !errors.Is(err, herdr.ErrEmptyTarget) {
		t.Fatalf("got %v", err)
	}
}

func TestTokenIsStableAndNamesEveryComponent(t *testing.T) {
	x := newFixture(t)
	a, err := x.g.Binding(context.Background(), "w1:p2")
	if err != nil {
		t.Fatal(err)
	}
	b, err := x.g.Binding(context.Background(), "w1:p2")
	if err != nil || a.Token != b.Token {
		t.Fatalf("token unstable: %s %s %v", a.Token, b.Token, err)
	}
	base := token("term_a", leaderPID, started, nativeID)
	if a.Token != base {
		t.Fatal("token differs from its components")
	}
	for name, other := range map[string]string{
		"terminal": token("term_b", leaderPID, started, nativeID),
		"pid":      token("term_a", leaderPID+1, started, nativeID),
		"sec":      token("term_a", leaderPID, StartTime{started.Sec + 1, started.Usec}, nativeID),
		"usec":     token("term_a", leaderPID, StartTime{started.Sec, started.Usec + 1}, nativeID),
		"session":  token("term_a", leaderPID, started, "00000000-0000-4000-8000-0000000000bb"),
		"boundary": token("term_a\x00"+fmt.Sprint(leaderPID), leaderPID, started, nativeID),
	} {
		if other == base {
			t.Fatalf("%s change kept the token", name)
		}
	}
	x.f.terminal = "term_b"
	if c, err := x.g.Binding(context.Background(), "w1:p2"); err != nil || c.Token == a.Token || c.TerminalID != "term_b" {
		t.Fatalf("terminal change kept the binding: %+v %v", c, err)
	}
}

func TestProcStartLayout(t *testing.T) {
	for _, tc := range []struct {
		at   time.Time
		want string
	}{
		{time.Date(2026, 10, 15, 8, 36, 5, 0, time.UTC), "Thu Oct 15 08:36:05 2026"},
		{time.Date(2026, 3, 1, 23, 0, 9, 0, time.UTC), "Sun Mar  1 23:00:09 2026"},
		{time.Unix(0, 0), "Thu Jan  1 00:00:00 1970"},
	} {
		if got := procStart(StartTime{Sec: tc.at.Unix(), Usec: 999999}); got != tc.want {
			t.Fatalf("procStart(%s) = %q, want %q", tc.at, got, tc.want)
		}
	}
}

func TestPromptSendsTextThenRechecksThenEnter(t *testing.T) {
	fastSettle(t)
	x := newFixture(t)
	b, err := x.g.Binding(context.Background(), "w1:p2")
	if err != nil {
		t.Fatal(err)
	}
	x.f.calls = nil
	if err := x.g.BoundInput(context.Background(), "w1:p2", b.Token, "prompt", "line one\n\tline two"); err != nil {
		t.Fatal(err)
	}
	want := "agent.get,pane.process_info,pane.process_info,pane.send_input,agent.get,pane.process_info,pane.process_info,pane.send_keys"
	if got := strings.Join(x.f.calls, ","); got != want {
		t.Fatalf("calls %s\nwant  %s", got, want)
	}
	if got := x.f.sent(); !reflect.DeepEqual(got, []sent{{"pane.send_input", "line one\n\tline two", nil}, {"pane.send_keys", "", []string{"enter"}}}) {
		t.Fatalf("sent %+v", got)
	}
}

func TestPromptWithholdsEnterWhenSessionChangesAfterText(t *testing.T) {
	fastSettle(t)
	moved := claudeInfo()
	moved.ForegroundProcessGroupID = u32(10)
	for name, change := range map[string]func(x fixture){
		"foreground changed": func(x fixture) { x.f.info = moved },
		"terminal changed":   func(x fixture) { x.f.terminal = "term_b" },
		"process restarted":  func(x fixture) { x.clock.answers = []StartTime{{Sec: started.Sec + 5}} },
		"herdr gone":         func(x fixture) { x.f.infoErr = errors.New("dial unix: connection refused") },
	} {
		t.Run(name, func(t *testing.T) {
			x := newFixture(t)
			b, err := x.g.Binding(context.Background(), "w1:p2")
			if err != nil {
				t.Fatal(err)
			}
			x.f.afterSend = func(method string) {
				if method == "pane.send_input" {
					x.f.mu.Lock()
					defer x.f.mu.Unlock()
					change(x)
				}
			}
			err = x.g.BoundInput(context.Background(), "w1:p2", b.Token, "prompt", "hello")
			var rejected *Rejected
			if !errors.Is(err, ErrDeliveryUnknown) || errors.As(err, &rejected) {
				t.Fatalf("got %v", err)
			}
			if got := x.f.sent(); len(got) != 1 || got[0].Method != "pane.send_input" {
				t.Fatalf("Enter sent after the session changed: %+v", got)
			}
		})
	}
}

func TestPromptFailuresAfterTextAreDeliveryUnknown(t *testing.T) {
	fastSettle(t)
	x := newFixture(t)
	b, _ := x.g.Binding(context.Background(), "w1:p2")
	x.f.sendErr["pane.send_keys"] = &herdr.APIError{Code: "pane_send_failed", Message: "queue full"}
	if err := x.g.BoundInput(context.Background(), "w1:p2", b.Token, "prompt", "hello"); !errors.Is(err, ErrDeliveryUnknown) {
		t.Fatalf("Enter failure: %v", err)
	}
	x = newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	x.f.afterSend = func(method string) {
		if method == "pane.send_input" {
			cancel()
		}
	}
	if err := x.g.BoundInput(ctx, "w1:p2", b.Token, "prompt", "hello"); !errors.Is(err, ErrDeliveryUnknown) {
		t.Fatalf("cancel during settle: %v", err)
	}
	if got := x.f.sent(); len(got) != 1 {
		t.Fatalf("Enter sent after cancel: %+v", got)
	}
}

func TestFailuresBeforeAnySendAreRejections(t *testing.T) {
	x := newFixture(t)
	b, _ := x.g.Binding(context.Background(), "w1:p2")
	for _, tc := range []struct {
		name  string
		setup func(x fixture)
		token string
		kind  string
		code  string
	}{
		{"stale token", func(fixture) {}, "stale", "prompt", CodeBindingMismatch},
		{"empty token", func(fixture) {}, "", "terminal_input", CodeBindingMismatch},
		{"session ended", func(x fixture) { x.f.info = herdr.ProcessInfo{ShellPID: u32(10), ForegroundProcessGroupID: u32(10)} }, b.Token, "prompt", CodeSessionEnded},
		{"pane gone", func(x fixture) { x.f.agentErr = &herdr.APIError{Code: "agent_not_found", Message: "gone"} }, b.Token, "interrupt", CodeSessionEnded},
		{"herdr unreachable", func(x fixture) { x.f.infoErr = errors.New("dial unix: connection refused") }, b.Token, "terminal_input", CodeSessionEnded},
		{"send pane_not_found", func(x fixture) {
			x.f.sendErr["pane.send_input"] = &herdr.APIError{Code: "pane_not_found", Message: "gone"}
		}, b.Token, "prompt", CodeSessionEnded},
		{"send canonical overflow", func(x fixture) {
			x.f.sendErr["pane.send_input"] = &herdr.APIError{Code: "pane_send_failed", Message: "canonical limit"}
		}, b.Token, "prompt", CodeInputRejected},
		{"interrupt rejected", func(x fixture) {
			x.f.sendErr["pane.send_keys"] = &herdr.APIError{Code: "pane_send_failed", Message: "closed"}
		}, b.Token, "interrupt", CodeInputRejected},
		{"unknown kind", func(fixture) {}, b.Token, "resize", CodeInputRejected},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x := newFixture(t)
			tc.setup(x)
			err := x.g.BoundInput(context.Background(), "w1:p2", tc.token, tc.kind, "hello")
			var rejected *Rejected
			if !errors.As(err, &rejected) || rejected.Code != tc.code || errors.Is(err, ErrDeliveryUnknown) {
				t.Fatalf("got %v, want rejection %s", err, tc.code)
			}
			if got := x.f.sent(); len(got) > 1 {
				t.Fatalf("more than one write before rejection: %+v", got)
			}
		})
	}
}

func TestFirstSendTransportFailureIsDeliveryUnknown(t *testing.T) {
	for _, kind := range []string{"prompt", "interrupt", "terminal_input"} {
		x := newFixture(t)
		b, _ := x.g.Binding(context.Background(), "w1:p2")
		for _, m := range []string{"pane.send_input", "pane.send_keys", "pane.send_text"} {
			x.f.sendErr[m] = context.DeadlineExceeded
		}
		if err := x.g.BoundInput(context.Background(), "w1:p2", b.Token, kind, "hello"); !errors.Is(err, ErrDeliveryUnknown) {
			t.Fatalf("%s: got %v", kind, err)
		}
		if got := x.f.sent(); len(got) != 1 {
			t.Fatalf("%s retried: %+v", kind, got)
		}
	}
}

func TestUnsupportedSendMethodIsNotAnUncertainDelivery(t *testing.T) {
	x := newFixture(t)
	b, _ := x.g.Binding(context.Background(), "w1:p2")
	x.f.sendErr["pane.send_text"] = fmt.Errorf("%w: pane.send_text", herdr.ErrUnsupported)
	err := x.g.BoundInput(context.Background(), "w1:p2", b.Token, "terminal_input", "x")
	if !errors.Is(err, herdr.ErrUnsupported) || errors.Is(err, ErrDeliveryUnknown) {
		t.Fatalf("got %v", err)
	}
}

func TestPromptValidation(t *testing.T) {
	fastSettle(t)
	for name, text := range map[string]string{
		"empty":        "",
		"too large":    strings.Repeat("a", maxInputBytes+1),
		"escape":       "hi\x1b[A",
		"carriage ret": "hi\rthere",
		"nul":          "hi\x00",
		"c1 control":   "hi\u0085",
		"invalid utf8": "hi\xff",
	} {
		t.Run(name, func(t *testing.T) {
			x := newFixture(t)
			b, _ := x.g.Binding(context.Background(), "w1:p2")
			err := x.g.BoundInput(context.Background(), "w1:p2", b.Token, "prompt", text)
			var rejected *Rejected
			if !errors.As(err, &rejected) || rejected.Code != CodeInvalidPrompt || len(x.f.sent()) != 0 {
				t.Fatalf("got %v sent %d", err, len(x.f.sent()))
			}
		})
	}
	for name, text := range map[string]string{
		"newline and tab": "a\n\tb",
		"at cap":          strings.Repeat("가", maxInputBytes/3) + "a",
	} {
		t.Run(name, func(t *testing.T) {
			x := newFixture(t)
			b, _ := x.g.Binding(context.Background(), "w1:p2")
			if err := x.g.BoundInput(context.Background(), "w1:p2", b.Token, "prompt", text); err != nil {
				t.Fatal(err)
			}
			if got := x.f.sent(); len(got) != 2 || got[0].Text != text {
				t.Fatalf("sent %+v", got)
			}
		})
	}
}

func TestTerminalInputAndInterrupt(t *testing.T) {
	x := newFixture(t)
	b, _ := x.g.Binding(context.Background(), "w1:p2")
	if err := x.g.BoundInput(context.Background(), "w1:p2", b.Token, "terminal_input", "\x1b[A\r"); err != nil {
		t.Fatal(err)
	}
	if err := x.g.BoundInput(context.Background(), "w1:p2", b.Token, "interrupt", "ignored"); err != nil {
		t.Fatal(err)
	}
	want := []sent{{"pane.send_text", "\x1b[A\r", nil}, {"pane.send_keys", "", []string{"ctrl+c"}}}
	if got := x.f.sent(); !reflect.DeepEqual(got, want) {
		t.Fatalf("sent %+v", got)
	}
	for _, text := range []string{"", strings.Repeat("x", maxInputBytes+1)} {
		err := x.g.BoundInput(context.Background(), "w1:p2", b.Token, "terminal_input", text)
		var rejected *Rejected
		if !errors.As(err, &rejected) || rejected.Code != CodeInvalidInput {
			t.Fatalf("raw input of %d bytes: %v", len(text), err)
		}
	}
	if got := x.f.sent(); len(got) != 2 {
		t.Fatalf("invalid raw input was sent: %+v", got)
	}
}
