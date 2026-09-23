package session

import (
	"context"
	"herdr-remote/internal/claude"
	"herdr-remote/internal/herdr"
	"herdr-remote/internal/stream"
	"herdr-remote/internal/transcript"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type fakeHerdr struct {
	agents   []herdr.Agent
	bindings map[string]herdr.Binding
}

func (f *fakeHerdr) Snapshot(context.Context) (herdr.Snapshot, error) {
	return herdr.Snapshot{Version: "0.9.1", Protocol: 22, Agents: f.agents}, nil
}
func (f *fakeHerdr) Binding(_ context.Context, p string) (herdr.Binding, error) {
	return f.bindings[p], nil
}
func (f *fakeHerdr) BoundInput(context.Context, string, string, string, string) error { return nil }
func (f *fakeHerdr) TerminalSnapshot(_ context.Context, paneID string) (herdr.TerminalSnapshot, error) {
	return herdr.TerminalSnapshot{PaneID: paneID, Source: "visible", Format: "ansi"}, nil
}
func TestDiscoverExistingNativeTranscriptAndRejectOldBinding(t *testing.T) {
	root := t.TempDir()
	id := "11111111-1111-4111-8111-111111111111"
	dir := filepath.Join(root, "projects", "repo")
	os.MkdirAll(dir, 0700)
	os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(`{"type":"user","uuid":"u1","parentUuid":null,"sessionId":"`+id+`","message":{"role":"user","content":"existing"}}`+"\n"), 0600)
	a := herdr.Agent{PaneID: "w1:p2", TerminalID: "term_a", Agent: "claude", Status: "idle"}
	f := &fakeHerdr{agents: []herdr.Agent{a}, bindings: map[string]herdr.Binding{a.PaneID: {Token: "A", TerminalID: a.TerminalID, NativeSessionID: id, ProcessID: 12, Agent: "claude", CWD: "repo"}}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := NewRegistry(ctx, f, root)
	if e := r.Refresh(ctx); e != nil {
		t.Fatal(e)
	}
	items := r.List()
	if len(items) != 1 || items[0].ID != "claude:"+id || !items[0].Chat {
		t.Fatalf("not discovered %+v", items)
	}
	deadline := time.After(2 * time.Second)
	for {
		snap, _, e := r.Get(items[0].ID)
		if e != nil {
			t.Fatal(e)
		}
		if string(snap.Data) != "[]" && len(snap.Data) > 3 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("history not loaded")
		case <-time.After(10 * time.Millisecond):
		}
	}
	f.bindings[a.PaneID] = herdr.Binding{Token: "B", TerminalID: a.TerminalID, NativeSessionID: id, ProcessID: 13, Agent: "claude", CWD: "repo"}
	if e := r.Refresh(ctx); e != nil {
		t.Fatal(e)
	}
	if r.List()[0].Binding != "B" {
		t.Fatal("new binding not tracked")
	}
}

func TestMixedNativeSessionNeverPublishesPartialHistory(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	item := &Item{nativeID: id, projection: claude.NewProjection(), stream: stream.New("claude:" + id)}
	valid := []byte(`{"type":"user","uuid":"u1","parentUuid":null,"sessionId":"` + id + `","message":{"role":"user","content":"private"}}`)
	wrong := []byte(`{"type":"user","uuid":"u2","parentUuid":"u1","sessionId":"22222222-2222-4222-8222-222222222222","message":{"role":"user","content":"other session"}}`)
	if err := item.apply(transcript.Batch{Reset: true, Lines: [][]byte{valid, wrong}}); err == nil {
		t.Fatal("mixed source accepted")
	}
	if got := string(item.stream.Snapshot().Data); got != "{}" {
		t.Fatalf("partial history leaked: %s", got)
	}
	if err := item.apply(transcript.Batch{}); err == nil {
		t.Fatal("invalid source recovered without reset")
	}
}

func TestInvalidTranscriptDisablesChatWriteButKeepsTerminal(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	item := &Item{nativeID: id, meta: Meta{ID: "claude:" + id, PaneID: "w1:p2", Binding: "bound", Chat: true, Terminal: true, Active: true, Status: "idle"}, projection: claude.NewProjection(), stream: stream.New("claude:" + id)}
	registry := &Registry{gateway: &fakeHerdr{}, items: map[string]*Item{item.meta.ID: item}}
	wrong := []byte(`{"type":"user","uuid":"wrong","sessionId":"22222222-2222-4222-8222-222222222222","message":{"role":"user","content":"other"}}`)
	if err := item.apply(transcript.Batch{Reset: true, Lines: [][]byte{wrong}}); err == nil {
		t.Fatal("wrong transcript accepted")
	}
	if got := registry.List()[0]; got.Chat || !got.Terminal {
		t.Fatalf("wrong fallback capabilities %+v", got)
	}
	if err := registry.BoundInput(context.Background(), item.meta.ID, "bound", "prompt", "unsafe"); err == nil {
		t.Fatal("Chat prompt remained enabled")
	}
	if err := registry.BoundInput(context.Background(), item.meta.ID, "bound", "terminal_input", "\x1b"); err != nil {
		t.Fatalf("terminal fallback disabled: %v", err)
	}
}

func TestVerifiedBindingKeepsTerminalWhenTranscriptIsMissing(t *testing.T) {
	root := t.TempDir()
	a := herdr.Agent{PaneID: "w1:p2", TerminalID: "term-a", Agent: "claude", Status: "idle"}
	f := &fakeHerdr{agents: []herdr.Agent{a}, bindings: map[string]herdr.Binding{a.PaneID: {Token: "bound", TerminalID: a.TerminalID, NativeSessionID: "11111111-1111-4111-8111-111111111111", ProcessID: 12, Agent: "claude", CWD: "repo"}}}
	r := NewRegistry(context.Background(), f, root)
	if err := r.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	item := r.List()[0]
	if item.Chat || !item.Terminal || item.Binding != "bound" {
		t.Fatalf("lost safe Terminal fallback %+v", item)
	}
	if err := r.BoundInput(context.Background(), item.ID, "bound", "terminal_input", "\x1b"); err != nil {
		t.Fatal(err)
	}
	if err := r.BoundInput(context.Background(), item.ID, "bound", "prompt", "unsafe"); err == nil {
		t.Fatal("Chat prompt enabled without transcript")
	}
}

func TestTerminalRejectsPaneReplacedAfterClientOpened(t *testing.T) {
	item := &Item{meta: Meta{ID: "claude:old", PaneID: "w1:p2", Binding: "A", Terminal: true, Active: true}}
	f := &fakeHerdr{bindings: map[string]herdr.Binding{"w1:p2": {Token: "B", TerminalID: "term-b", NativeSessionID: "new", ProcessID: 22, Agent: "claude"}}}
	r := &Registry{gateway: f, items: map[string]*Item{item.meta.ID: item}}
	if _, err := r.Terminal(context.Background(), item.meta.ID, "A"); err == nil {
		t.Fatal("old session displayed replacement pane")
	}
}
