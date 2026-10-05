package session

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/psw7205/herdr-remote/internal/herdr"
	"github.com/psw7205/herdr-remote/internal/stream"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"
)

const (
	codexA = "01a0aaaa-0000-7000-8000-00000000000a"
	codexB = "01a0aaaa-0000-7000-8000-00000000000b"
)

// recordingHerdr records every agent.binding target so tests can prove that
// Codex panes never reach it.
type recordingHerdr struct {
	*fakeHerdr
	mu    sync.Mutex
	asked []string
}

func (g *recordingHerdr) Binding(ctx context.Context, pane string) (herdr.Binding, error) {
	g.mu.Lock()
	g.asked = append(g.asked, pane)
	g.mu.Unlock()
	return g.fakeHerdr.Binding(ctx, pane)
}

func (g *recordingHerdr) bindingAsked(pane string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return slices.Contains(g.asked, pane)
}

func codexAgent(pane, native string) herdr.Agent {
	a := herdr.Agent{PaneID: pane, TerminalID: "term-" + pane, Agent: "codex", Status: "idle", CWD: "repo"}
	if native != "" {
		a.Session = &herdr.NativeSession{Source: "herdr:codex", Agent: "codex", Kind: "id", Value: native}
	}
	return a
}

func rolloutLine(ordinal int, typ, payload string) string {
	return fmt.Sprintf(`{"timestamp":"2026-10-01T00:00:%02d.000Z","type":%q,"ordinal":%d,"payload":%s}`, ordinal, typ, ordinal, payload) + "\n"
}

func codexMeta(id string) string {
	return rolloutLine(0, "session_meta", `{"id":"`+id+`","source":"cli","originator":"codex_cli_rs","cwd":"repo"}`)
}

func codexInjected(ordinal int) string {
	return rolloutLine(ordinal, "response_item", `{"type":"message","role":"user","content":[{"type":"input_text","text":"injected sample"},{"type":"input_text","text":"environment sample"}],"internal_chat_message_metadata_passthrough":{"content_item_kinds":["agents_md.instructions","environments.environment_context"]}}`)
}

func codexTyped(ordinal int, text string) string {
	return rolloutLine(ordinal, "response_item", `{"type":"message","role":"user","content":[{"type":"input_text","text":"`+text+`"}],"internal_chat_message_metadata_passthrough":{"content_item_kinds":["user.text"]}}`)
}

func codexAnswer(ordinal int, text string) string {
	return rolloutLine(ordinal, "response_item", `{"type":"message","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"`+text+`"}],"internal_chat_message_metadata_passthrough":{"content_item_kinds":["unknown"]}}`) +
		rolloutLine(ordinal+1, "event_msg", `{"type":"item_completed","item":{"type":"agent_message","text":"`+text+`"}}`) +
		rolloutLine(ordinal+2, "event_msg", `{"type":"task_complete","last_agent_message":"`+text+`"}`)
}

func writeRollout(t *testing.T, root, id string, lines ...string) string {
	t.Helper()
	dir := filepath.Join(root, "sessions", "2026", "10", "01")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "rollout-2026-10-01T00-00-00-"+id+".jsonl")
	data := ""
	for _, l := range lines {
		data += l
	}
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func appendRollout(t *testing.T, path, data string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(data); err != nil {
		t.Fatal(err)
	}
}

func messagesOf(t *testing.T, snap stream.Snapshot) []string {
	t.Helper()
	var messages []Message
	if err := json.Unmarshal(snap.Data, &messages); err != nil {
		t.Fatalf("snapshot %s: %v", snap.Data, err)
	}
	var out []string
	for _, m := range messages {
		out = append(out, m.ID+":"+m.Role+":"+m.Text)
	}
	return out
}

func waitMessages(t *testing.T, r *Registry, id string, want ...string) stream.Snapshot {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		snap, _, err := r.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if string(snap.Data) != "{}" && slices.Equal(messagesOf(t, snap), want) {
			return snap
		}
		select {
		case <-deadline:
			t.Fatalf("%s history %s, want %v", id, snap.Data, want)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func codexRegistry(t *testing.T, agents ...herdr.Agent) (*Registry, *recordingHerdr, string) {
	t.Helper()
	root := t.TempDir()
	g := &recordingHerdr{fakeHerdr: &fakeHerdr{agents: agents, bindings: map[string]herdr.Binding{}}}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return NewRegistry(ctx, g, t.TempDir(), WithCodexRoot(root)), g, root
}

func TestCodexSessionIsReadOnlyChatFromAgentSession(t *testing.T) {
	a := codexAgent("w1:p3", codexA)
	r, g, root := codexRegistry(t, a)
	writeRollout(t, root, codexA, codexMeta(codexA), codexInjected(1), codexTyped(2, "typed sample"), codexAnswer(3, "answer sample"))
	ctx := context.Background()
	if err := r.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	id := "codex:" + codexA
	items := r.List()
	if len(items) != 1 || items[0].ID != id || items[0].Agent != "codex" || !items[0].Chat || items[0].Terminal || items[0].Binding != "" || items[0].Lifecycle != LifecycleUnbound {
		t.Fatalf("codex item %+v", items)
	}
	waitMessages(t, r, id, "2:user:typed sample", "3:assistant:answer sample")
	if _, meta, _ := r.Get(id); meta.LastMessage == nil || meta.LastMessage.Text != "answer sample" {
		t.Fatalf("preview %+v", meta)
	}

	for _, token := range []string{"", "forged"} {
		for _, kind := range []string{"prompt", "interrupt", "terminal_input"} {
			if err := r.BoundInput(ctx, id, token, kind, "x"); err == nil {
				t.Fatalf("%s accepted with binding %q", kind, token)
			}
		}
		if _, err := r.Terminal(ctx, id, token); err == nil {
			t.Fatalf("Terminal read accepted with binding %q", token)
		}
	}
	if g.inputs != 0 || g.bindingAsked(a.PaneID) {
		t.Fatalf("codex pane reached Herdr writes or agent.binding: inputs=%d asked=%v", g.inputs, g.asked)
	}
	if got := r.ConditionalInput(); got != herdr.ConditionalInputUnknown {
		t.Fatalf("codex-only snapshot judged conditional input %s", got)
	}
}

// Codex panes must not feed the server-wide capability that Claude writes use.
func TestCodexPaneDoesNotAffectConditionalInput(t *testing.T) {
	claudePane := herdr.Agent{PaneID: "w1:p2", TerminalID: "term_a", Agent: "claude", Status: "idle"}
	r, g, _ := codexRegistry(t, claudePane, codexAgent("w1:p3", codexA))
	g.bindingErr = &herdr.APIError{Code: "binding_unavailable", Message: "no native session"}
	if err := r.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !g.bindingAsked(claudePane.PaneID) || g.bindingAsked("w1:p3") {
		t.Fatalf("agent.binding targets %v", g.asked)
	}
	if got := r.ConditionalInput(); got != herdr.ConditionalInputSupported {
		t.Fatalf("conditional input %s", got)
	}
}

// Without a report, or before the rollout exists, a Codex pane is a
// status-only pane: item; the reported session then supersedes it.
func TestCodexPaneWithoutSessionIsStatusOnlyUntilReported(t *testing.T) {
	a := codexAgent("w1:p3", "")
	r, g, root := codexRegistry(t, a)
	ctx := context.Background()
	if err := r.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	pane := "pane:w1:p3"
	if items := r.List(); len(items) != 1 || items[0].ID != pane || items[0].Chat || items[0].Terminal || items[0].Lifecycle != LifecycleUnbound || watcherOf(r, pane) != nil {
		t.Fatalf("status-only item %+v", items)
	}
	events := statusEvents(t, r, pane)

	g.agents = []herdr.Agent{codexAgent("w1:p3", codexA)}
	if err := r.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if items := r.List(); len(items) != 1 || items[0].ID != pane {
		t.Fatalf("reported session without rollout %+v", items)
	}

	writeRollout(t, root, codexA, codexMeta(codexA), codexTyped(1, "typed sample"))
	if err := r.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	id := "codex:" + codexA
	if items := r.List(); len(items) != 1 || items[0].ID != id {
		t.Fatalf("not re-keyed %+v", items)
	}
	if _, meta, _ := r.Get(pane); meta.Lifecycle != LifecycleSuperseded || meta.SuccessorID != id {
		t.Fatalf("pane item %+v", meta)
	}
	if got := events(); !slices.Equal(got, []string{LifecycleSuperseded + ">" + id}) {
		t.Fatalf("status events %v", got)
	}
	waitMessages(t, r, id, "1:user:typed sample")
}

func TestCodexSessionEndsOnAnotherReportedSession(t *testing.T) {
	r, g, root := codexRegistry(t, codexAgent("w1:p3", codexA))
	writeRollout(t, root, codexA, codexMeta(codexA), codexTyped(1, "first sample"))
	writeRollout(t, root, codexB, codexMeta(codexB), codexTyped(1, "second sample"))
	ctx := context.Background()
	if err := r.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	first := "codex:" + codexA
	waitMessages(t, r, first, "1:user:first sample")

	// A missing report keeps the session on its pane.
	g.agents = []herdr.Agent{codexAgent("w1:p3", "")}
	if err := r.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if items := r.List(); len(items) != 1 || items[0].ID != first || !items[0].Chat {
		t.Fatalf("lost session without report %+v", items)
	}

	// Another reported session on the same pane ends the first.
	watcher := watcherOf(r, first)
	g.agents = []herdr.Agent{codexAgent("w1:p3", codexB)}
	if err := r.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	second := "codex:" + codexB
	if items := r.List(); len(items) != 1 || items[0].ID != second {
		t.Fatalf("second session %+v", items)
	}
	if _, meta, _ := r.Get(first); meta.Lifecycle != LifecycleEnded {
		t.Fatalf("first session %+v", meta)
	}
	waitClosed(t, watcher)
	waitMessages(t, r, second, "1:user:second sample")

	// A report of an unknown session without a rollout also ends it.
	g.agents = []herdr.Agent{codexAgent("w1:p3", "01a0aaaa-0000-7000-8000-00000000000c")}
	if err := r.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if items := r.List(); len(items) != 1 || items[0].ID != "pane:w1:p3" {
		t.Fatalf("unknown session %+v", items)
	}
	if _, meta, _ := r.Get(second); meta.Lifecycle != LifecycleEnded {
		t.Fatalf("second session %+v", meta)
	}

	// Herdr no longer reporting Codex on the pane ends the item too.
	g.agents = []herdr.Agent{codexAgent("w1:p3", codexB)}
	if err := r.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	g.agents = []herdr.Agent{{PaneID: "w1:p3", TerminalID: "term-w1:p3", Agent: "claude", Status: "idle"}}
	if err := r.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if _, meta, _ := r.Get(second); meta.Lifecycle != LifecycleEnded {
		t.Fatalf("agent change kept codex session %+v", meta)
	}
	if items := r.List(); len(items) != 1 || items[0].ID != "pane:w1:p3" || items[0].Agent != "claude" {
		t.Fatalf("claude pane after codex %+v", items)
	}
}

func TestCodexSessionReportedOnTwoPanesClaimsNothing(t *testing.T) {
	r, _, root := codexRegistry(t, codexAgent("w1:p3", codexA), codexAgent("w1:p4", codexA))
	writeRollout(t, root, codexA, codexMeta(codexA))
	if err := r.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	items := r.List()
	if len(items) != 2 || items[0].ID != "pane:w1:p3" || items[1].ID != "pane:w1:p4" || items[0].Chat || items[1].Chat {
		t.Fatalf("ambiguous report %+v", items)
	}
}

// The watcher publishes a record only once its line is complete, resyncs a
// replaced rollout under a new epoch, and replays to a reconnecting cursor
// without the event_msg copies of a message.
func TestCodexWatcherPartialReplaceAndReconnect(t *testing.T) {
	r, _, root := codexRegistry(t, codexAgent("w1:p3", codexA))
	path := writeRollout(t, root, codexA, codexMeta(codexA), codexTyped(1, "typed sample"))
	if err := r.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	id := "codex:" + codexA
	snap := waitMessages(t, r, id, "1:user:typed sample")

	answer := codexAnswer(2, "answer sample")
	appendRollout(t, path, answer[:len(answer)/3])
	time.Sleep(1300 * time.Millisecond)
	if now, _, _ := r.Get(id); now.Cursor != snap.Cursor || !slices.Equal(messagesOf(t, now), []string{"1:user:typed sample"}) {
		t.Fatalf("partial line published: %+v %s", now.Cursor, now.Data)
	}
	appendRollout(t, path, answer[len(answer)/3:])
	live := waitMessages(t, r, id, "1:user:typed sample", "2:assistant:answer sample")
	if live.Cursor.Epoch != snap.Cursor.Epoch {
		t.Fatal("append changed the epoch")
	}

	s, err := r.Stream(id)
	if err != nil {
		t.Fatal(err)
	}
	replay, _, stop := s.Subscribe(snap.Cursor)
	stop()
	if len(replay) != 1 || replay[0].Type != "message.assistant" || replay[0].Source != "codex.transcript" {
		t.Fatalf("replay %+v", replay)
	}
	resync, _, stop := s.Subscribe(stream.Cursor{Epoch: "foreign", Sequence: 1})
	stop()
	if len(resync) != 1 || resync[0].Type != "session.snapshot" {
		t.Fatalf("foreign cursor %+v", resync)
	}

	next := path + ".next"
	if err := os.WriteFile(next, []byte(codexMeta(codexA)+codexTyped(1, "rewritten sample")), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(next, path); err != nil {
		t.Fatal(err)
	}
	replaced := waitMessages(t, r, id, "1:user:rewritten sample")
	if replaced.Cursor.Epoch == live.Cursor.Epoch {
		t.Fatal("replaced rollout kept the old epoch")
	}
}

func TestCodexRolloutOfAnotherSessionInvalidatesChat(t *testing.T) {
	r, _, root := codexRegistry(t, codexAgent("w1:p3", codexA))
	path := writeRollout(t, root, codexA, codexMeta(codexA), codexTyped(1, "typed sample"))
	if err := r.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	id := "codex:" + codexA
	waitMessages(t, r, id, "1:user:typed sample")
	appendRollout(t, path, codexMeta(codexB))
	deadline := time.After(3 * time.Second)
	for {
		if _, meta, _ := r.Get(id); !meta.Chat {
			break
		}
		select {
		case <-deadline:
			t.Fatal("foreign session_meta kept Chat")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestCodexWithoutRootListsStatusOnly(t *testing.T) {
	g := &fakeHerdr{agents: []herdr.Agent{codexAgent("w1:p3", codexA)}}
	r := NewRegistry(context.Background(), g, t.TempDir())
	if err := r.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if items := r.List(); len(items) != 1 || items[0].ID != "pane:w1:p3" || items[0].Chat {
		t.Fatalf("codex without root %+v", items)
	}
}
