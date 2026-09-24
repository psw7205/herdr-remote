package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"herdr-remote/internal/claude"
	"herdr-remote/internal/herdr"
	"herdr-remote/internal/stream"
	"herdr-remote/internal/transcript"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeHerdr struct {
	agents      []herdr.Agent
	bindings    map[string]herdr.Binding
	bindingErr  error
	snapshotErr error
	inputs      int
}

func (f *fakeHerdr) Snapshot(context.Context) (herdr.Snapshot, error) {
	if f.snapshotErr != nil {
		return herdr.Snapshot{}, f.snapshotErr
	}
	return herdr.Snapshot{Version: "0.9.1", Protocol: 22, Agents: f.agents}, nil
}
func (f *fakeHerdr) Binding(_ context.Context, p string) (herdr.Binding, error) {
	if f.bindingErr != nil {
		return herdr.Binding{}, f.bindingErr
	}
	return f.bindings[p], nil
}
func TestConditionalInputCapabilityFailsClosed(t *testing.T) {
	a := herdr.Agent{PaneID: "w1:p2", TerminalID: "term_a", Agent: "claude", Status: "idle"}
	f := &fakeHerdr{}
	ctx := context.Background()
	r := NewRegistry(ctx, f, t.TempDir())
	if e := r.Refresh(ctx); e != nil || r.ConditionalInput() != "unknown" {
		t.Fatalf("no agents: %v %s", e, r.ConditionalInput())
	}
	f.agents = []herdr.Agent{a}
	f.bindingErr = errors.New("dial unix: connection refused")
	if r.Refresh(ctx); r.ConditionalInput() != "unknown" {
		t.Fatalf("transport error counted as evidence: %s", r.ConditionalInput())
	}
	f.bindingErr = &herdr.APIError{Code: "binding_unavailable", Message: "no native session"}
	if r.Refresh(ctx); r.ConditionalInput() != "supported" {
		t.Fatalf("method-level error: %s", r.ConditionalInput())
	}
	f.bindingErr = fmt.Errorf("%w: agent.binding", herdr.ErrUnsupported)
	if r.Refresh(ctx); r.ConditionalInput() != "unsupported" {
		t.Fatalf("unsupported: %s", r.ConditionalInput())
	}
	items := r.List()
	if len(items) != 1 || items[0].Chat || items[0].Terminal || items[0].Binding != "" {
		t.Fatalf("unsupported Herdr enabled controls: %+v", items)
	}
	if e := r.BoundInput(ctx, items[0].ID, "", "terminal_input", "x"); e == nil {
		t.Fatal("unsupported Herdr accepted input")
	}
	f.snapshotErr = errors.New("snapshot failed")
	if e := r.Refresh(ctx); e == nil || r.ConditionalInput() != "unsupported" {
		t.Fatalf("snapshot failure changed capability: %v %s", e, r.ConditionalInput())
	}
}
func (f *fakeHerdr) BoundInput(context.Context, string, string, string, string) error {
	f.inputs++
	return nil
}
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

const (
	nativeA = "11111111-1111-4111-8111-111111111111"
	nativeB = "22222222-2222-4222-8222-222222222222"
	nativeC = "33333333-3333-4333-8333-333333333333"
)

func writeTranscript(t *testing.T, root, project, id string) {
	t.Helper()
	dir := filepath.Join(root, "projects", project)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	line := `{"type":"user","uuid":"u-` + id + `","parentUuid":null,"sessionId":"` + id + `","message":{"role":"user","content":"sample"}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(line), 0600); err != nil {
		t.Fatal(err)
	}
}

// boundRegistry returns a Registry whose pane w1:p2 is verified as
// claude:nativeA with binding "A" and whose history has finished loading.
func boundRegistry(t *testing.T) (*Registry, *fakeHerdr, string) {
	t.Helper()
	root := t.TempDir()
	writeTranscript(t, root, "repo", nativeA)
	writeTranscript(t, root, "repo", nativeB)
	a := herdr.Agent{PaneID: "w1:p2", TerminalID: "term_a", Agent: "claude", Status: "idle", CWD: "repo"}
	f := &fakeHerdr{agents: []herdr.Agent{a}, bindings: map[string]herdr.Binding{a.PaneID: {Token: "A", TerminalID: "term_a", NativeSessionID: nativeA, ProcessID: 12, Agent: "claude", CWD: "repo"}}}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	r := NewRegistry(ctx, f, root)
	if err := r.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(2 * time.Second)
	for {
		snap, meta, err := r.Get("claude:" + nativeA)
		if err != nil {
			t.Fatal(err)
		}
		if meta.Lifecycle != LifecycleActive || meta.Binding != "A" {
			t.Fatalf("not verified: %+v", meta)
		}
		if len(snap.Data) > 3 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("history not loaded")
		case <-time.After(10 * time.Millisecond):
		}
	}
	return r, f, root
}

// statusEvents subscribes live; a successor_id is appended as "lifecycle>id". so a transcript Reset cannot drop the events.
func statusEvents(t *testing.T, r *Registry, id string) func() []string {
	t.Helper()
	s, err := r.Stream(id)
	if err != nil {
		t.Fatal(err)
	}
	_, live, stop := s.Subscribe(s.Snapshot().Cursor)
	t.Cleanup(stop)
	return func() []string {
		var out []string
		for {
			select {
			case e := <-live:
				if e.Type == "agent.status" {
					var p struct {
						Lifecycle   string
						SuccessorID string `json:"successor_id"`
					}
					if err := json.Unmarshal(e.Payload, &p); err != nil {
						t.Fatal(err)
					}
					if p.SuccessorID != "" {
						p.Lifecycle += ">" + p.SuccessorID
					}
					out = append(out, p.Lifecycle)
				}
			default:
				return out
			}
		}
	}
}

func assertNoWrites(t *testing.T, r *Registry, f *fakeHerdr, id string) {
	t.Helper()
	for _, token := range []string{"A", ""} {
		for _, kind := range []string{"prompt", "interrupt", "terminal_input"} {
			if err := r.BoundInput(context.Background(), id, token, kind, "x"); err == nil {
				t.Fatalf("%s accepted with binding %q", kind, token)
			}
		}
		if _, err := r.Terminal(context.Background(), id, token); err == nil {
			t.Fatalf("Terminal read accepted with binding %q", token)
		}
	}
	if f.inputs != 0 {
		t.Fatalf("Herdr received %d writes", f.inputs)
	}
}

func TestBindingLossKeepsSessionUnverified(t *testing.T) {
	handoff := herdr.Agent{PaneID: "w1:p2", TerminalID: "term_b", Agent: "claude", Status: "working", CWD: "repo"}
	reported := handoff
	reported.Session = &herdr.NativeSession{Source: "herdr:claude", Agent: "claude", Kind: "id", Value: nativeA}
	unsupported := fmt.Errorf("%w: agent.binding", herdr.ErrUnsupported)
	cases := []struct {
		name    string
		agent   herdr.Agent
		binding herdr.Binding
		err     error
	}{
		{"stock Herdr after handoff", handoff, herdr.Binding{}, unsupported},
		{"transient transport error", handoff, herdr.Binding{}, errors.New("dial unix: connection refused")},
		{"method-level binding error", handoff, herdr.Binding{}, &herdr.APIError{Code: "binding_unavailable", Message: "no native session"}},
		{"snapshot and binding race", handoff, herdr.Binding{Token: "B", TerminalID: "term_c", NativeSessionID: nativeA, ProcessID: 12, Agent: "claude"}, nil},
		{"Herdr reports the same native session", reported, herdr.Binding{}, unsupported},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, f, _ := boundRegistry(t)
			id := "claude:" + nativeA
			events := statusEvents(t, r, id)
			f.agents = []herdr.Agent{tc.agent}
			f.bindings[tc.agent.PaneID] = tc.binding
			f.bindingErr = tc.err
			if err := r.Refresh(context.Background()); err != nil {
				t.Fatal(err)
			}
			items := r.List()
			if len(items) != 1 || items[0].ID != id {
				t.Fatalf("session identity not kept or duplicated: %+v", items)
			}
			got := items[0]
			if !got.Active || got.Lifecycle != LifecycleUnverified || got.Binding != "" || got.Terminal || !got.Chat || got.Status != "working" {
				t.Fatalf("wrong degraded meta: %+v", got)
			}
			if e := events(); !slices.Equal(e, []string{LifecycleUnverified}) {
				t.Fatalf("agent.status lifecycles %v", e)
			}
			assertNoWrites(t, r, f, id)
		})
	}
}

func TestBindingLossEndsOnlyOnConfirmedEnd(t *testing.T) {
	cases := []struct {
		name   string
		agents []herdr.Agent
		want   []string
	}{
		{"pane left the snapshot", nil, nil},
		{"Herdr no longer reports claude on the pane", []herdr.Agent{{PaneID: "w1:p2", TerminalID: "term_b", Status: "idle"}}, nil},
		{"Herdr reports another native session", []herdr.Agent{{PaneID: "w1:p2", TerminalID: "term_b", Agent: "claude", Status: "idle", Session: &herdr.NativeSession{Agent: "claude", Kind: "id", Value: nativeC}}}, []string{"pane:w1:p2"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, f, _ := boundRegistry(t)
			id := "claude:" + nativeA
			events := statusEvents(t, r, id)
			f.agents = tc.agents
			f.bindingErr = fmt.Errorf("%w: agent.binding", herdr.ErrUnsupported)
			if err := r.Refresh(context.Background()); err != nil {
				t.Fatal(err)
			}
			var ids []string
			for _, item := range r.List() {
				ids = append(ids, item.ID)
				if item.Lifecycle != LifecycleUnverified || item.Binding != "" {
					t.Fatalf("replacement item not fail closed: %+v", item)
				}
			}
			if !slices.Equal(ids, tc.want) {
				t.Fatalf("listed %v, want %v", ids, tc.want)
			}
			if _, meta, err := r.Get(id); err != nil || meta.Active || meta.Lifecycle != LifecycleEnded {
				t.Fatalf("old session not ended: %+v %v", meta, err)
			}
			if e := events(); !slices.Equal(e, []string{LifecycleEnded}) {
				t.Fatalf("agent.status lifecycles %v", e)
			}
			assertNoWrites(t, r, f, id)
		})
	}
}

func TestBindingRecovery(t *testing.T) {
	cases := []struct {
		name       string
		native     string
		ambiguous  bool
		wantID     string
		wantEvents []string
	}{
		{"same native session", nativeA, false, "claude:" + nativeA, []string{LifecycleUnverified, LifecycleActive}},
		{"same native session while transcript is ambiguous", nativeA, true, "claude:" + nativeA, []string{LifecycleUnverified, LifecycleActive}},
		{"different native session", nativeB, false, "claude:" + nativeB, []string{LifecycleUnverified, LifecycleEnded}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, f, root := boundRegistry(t)
			old := "claude:" + nativeA
			events := statusEvents(t, r, old)
			a := herdr.Agent{PaneID: "w1:p2", TerminalID: "term_b", Agent: "claude", Status: "idle", CWD: "repo"}
			f.agents = []herdr.Agent{a}
			f.bindingErr = fmt.Errorf("%w: agent.binding", herdr.ErrUnsupported)
			if err := r.Refresh(context.Background()); err != nil {
				t.Fatal(err)
			}
			if tc.ambiguous {
				// A second copy makes claude.Resolve fail without touching the watched file.
				writeTranscript(t, root, "other", nativeA)
			}
			f.bindingErr = nil
			f.bindings[a.PaneID] = herdr.Binding{Token: "B", TerminalID: "term_b", NativeSessionID: tc.native, ProcessID: 13, Agent: "claude", CWD: "repo"}
			if err := r.Refresh(context.Background()); err != nil {
				t.Fatal(err)
			}
			items := r.List()
			if len(items) != 1 || items[0].ID != tc.wantID || items[0].Lifecycle != LifecycleActive || items[0].Binding != "B" || !items[0].Terminal || !items[0].Chat {
				t.Fatalf("recovered sessions %+v", items)
			}
			if tc.wantID != old {
				if _, meta, _ := r.Get(old); meta.Active || meta.Lifecycle != LifecycleEnded {
					t.Fatalf("old session not ended: %+v", meta)
				}
			}
			if e := events(); !slices.Equal(e, tc.wantEvents) {
				t.Fatalf("agent.status lifecycles %v, want %v", e, tc.wantEvents)
			}
			if err := r.BoundInput(context.Background(), tc.wantID, "A", "terminal_input", "x"); err == nil {
				t.Fatal("stale binding accepted after recovery")
			}
			if err := r.BoundInput(context.Background(), tc.wantID, "B", "terminal_input", "x"); err != nil {
				t.Fatalf("recovered binding rejected: %v", err)
			}
		})
	}
}

func assertTokenRejected(t *testing.T, r *Registry, id string, tokens ...string) {
	t.Helper()
	for _, token := range tokens {
		if err := r.BoundInput(context.Background(), id, token, "terminal_input", "x"); err == nil {
			t.Fatalf("%s accepted binding %q", id, token)
		}
		if _, err := r.Terminal(context.Background(), id, token); err == nil {
			t.Fatalf("%s Terminal accepted binding %q", id, token)
		}
	}
}

func TestVerifiedObservationWinsOverContinuity(t *testing.T) {
	moved := herdr.Agent{PaneID: "w1:p3", TerminalID: "term_c", Agent: "claude", Status: "idle", CWD: "repo"}
	old := herdr.Agent{PaneID: "w1:p2", TerminalID: "term_b", Agent: "claude", Status: "idle", CWD: "repo"}
	for name, agents := range map[string][]herdr.Agent{"verified first": {moved, old}, "verified last": {old, moved}} {
		t.Run(name, func(t *testing.T) {
			r, f, _ := boundRegistry(t)
			id := "claude:" + nativeA
			f.agents = agents
			f.bindings[moved.PaneID] = herdr.Binding{Token: "B", TerminalID: "term_c", NativeSessionID: nativeA, ProcessID: 13, Agent: "claude", CWD: "repo"}
			f.bindings[old.PaneID] = herdr.Binding{}
			if err := r.Refresh(context.Background()); err != nil {
				t.Fatal(err)
			}
			_, meta, err := r.Get(id)
			if err != nil || meta.Lifecycle != LifecycleActive || meta.PaneID != moved.PaneID || meta.Binding != "B" {
				t.Fatalf("verified observation overridden: %+v %v", meta, err)
			}
			var ids []string
			for _, item := range r.List() {
				ids = append(ids, item.ID)
			}
			if !slices.Equal(ids, []string{id, "pane:w1:p2"}) {
				t.Fatalf("listed %v", ids)
			}
			if err := r.BoundInput(context.Background(), id, "B", "terminal_input", "x"); err != nil {
				t.Fatalf("verified binding rejected: %v", err)
			}
		})
	}
}

func TestRejectedBindingForAnotherNativeEndsContinuity(t *testing.T) {
	r, f, _ := boundRegistry(t)
	id := "claude:" + nativeA
	events := statusEvents(t, r, id)
	f.agents = []herdr.Agent{{PaneID: "w1:p2", TerminalID: "term_b", Agent: "claude", Status: "idle", CWD: "repo"}}
	// terminal_id race: the call succeeded but the binding is filtered out.
	f.bindings["w1:p2"] = herdr.Binding{Token: "B", TerminalID: "term_c", NativeSessionID: nativeB, ProcessID: 13, Agent: "claude", CWD: "repo"}
	if err := r.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if items := r.List(); len(items) != 1 || items[0].ID != "pane:w1:p2" || items[0].Lifecycle != LifecycleUnverified {
		t.Fatalf("listed %+v", items)
	}
	if _, meta, _ := r.Get(id); meta.Active || meta.Lifecycle != LifecycleEnded {
		t.Fatalf("continuity kept on another native session: %+v", meta)
	}
	if e := events(); !slices.Equal(e, []string{LifecycleEnded}) {
		t.Fatalf("agent.status lifecycles %v", e)
	}
	assertNoWrites(t, r, f, id)
}

func TestEndedSessionRevivesOnVerifiedBinding(t *testing.T) {
	r, f, _ := boundRegistry(t)
	id := "claude:" + nativeA
	events := statusEvents(t, r, id)
	a := herdr.Agent{PaneID: "w1:p2", TerminalID: "term_b", Agent: "claude", Status: "idle", CWD: "repo"}
	f.agents = []herdr.Agent{{PaneID: a.PaneID, TerminalID: a.TerminalID, Status: "idle"}}
	if err := r.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Ended items get no continuity, so the pane is re-listed as a pane: item.
	f.agents = []herdr.Agent{a}
	f.bindingErr = fmt.Errorf("%w: agent.binding", herdr.ErrUnsupported)
	if err := r.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if items := r.List(); len(items) != 1 || items[0].ID != "pane:w1:p2" || items[0].Lifecycle != LifecycleUnverified {
		t.Fatalf("listed %+v", items)
	}
	f.bindingErr = nil
	f.bindings[a.PaneID] = herdr.Binding{Token: "C", TerminalID: "term_b", NativeSessionID: nativeA, ProcessID: 14, Agent: "claude", CWD: "repo"}
	if err := r.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if items := r.List(); len(items) != 1 || items[0].ID != id || !items[0].Active || items[0].Lifecycle != LifecycleActive || items[0].Binding != "C" || !items[0].Chat || !items[0].Terminal {
		t.Fatalf("not revived %+v", items)
	}
	if e := events(); !slices.Equal(e, []string{LifecycleEnded, LifecycleActive}) {
		t.Fatalf("agent.status lifecycles %v", e)
	}
	assertTokenRejected(t, r, id, "A", "")
	if err := r.BoundInput(context.Background(), id, "C", "terminal_input", "x"); err != nil {
		t.Fatalf("revived binding rejected: %v", err)
	}
}

// Two active items share a pane only if one snapshot listed that pane twice
// and the per-pane agent.binding calls disagreed; continuity must not pick one.
func TestAmbiguousPaneGetsNoContinuity(t *testing.T) {
	f := &fakeHerdr{agents: []herdr.Agent{{PaneID: "w1:p2", TerminalID: "term_b", Agent: "claude", Status: "idle"}}, bindingErr: fmt.Errorf("%w: agent.binding", herdr.ErrUnsupported)}
	r := NewRegistry(context.Background(), f, t.TempDir())
	for _, native := range []string{nativeA, nativeB} {
		id := "claude:" + native
		r.items[id] = &Item{nativeID: native, stream: stream.New(id), projection: claude.NewProjection(), meta: Meta{ID: id, Agent: "claude", PaneID: "w1:p2", Status: "idle", Binding: native, Terminal: true, Active: true}}
	}
	if err := r.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if items := r.List(); len(items) != 1 || items[0].ID != "pane:w1:p2" || items[0].Lifecycle != LifecycleUnverified {
		t.Fatalf("listed %+v", items)
	}
	for _, native := range []string{nativeA, nativeB} {
		if _, meta, _ := r.Get("claude:" + native); meta.Active || meta.Lifecycle != LifecycleEnded {
			t.Fatalf("ambiguous item kept: %+v", meta)
		}
	}
}

// flappingHerdr alternates binding success and capability loss, so every
// Refresh mutates item meta and streams; it is safe for concurrent use.
type flappingHerdr struct {
	fakeHerdr
	calls atomic.Int64
}

func (f *flappingHerdr) Binding(ctx context.Context, p string) (herdr.Binding, error) {
	if f.calls.Add(1)%2 == 0 {
		return herdr.Binding{}, fmt.Errorf("%w: agent.binding", herdr.ErrUnsupported)
	}
	return f.fakeHerdr.Binding(ctx, p)
}

func TestConcurrentRefreshAndReads(t *testing.T) {
	root := t.TempDir()
	writeTranscript(t, root, "repo", nativeA)
	a := herdr.Agent{PaneID: "w1:p2", TerminalID: "term_a", Agent: "claude", Status: "idle", CWD: "repo"}
	f := &flappingHerdr{fakeHerdr: fakeHerdr{agents: []herdr.Agent{a}, bindings: map[string]herdr.Binding{a.PaneID: {Token: "A", TerminalID: "term_a", NativeSessionID: nativeA, ProcessID: 12, Agent: "claude", CWD: "repo"}}}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := NewRegistry(ctx, f, root)
	if err := r.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	id := "claude:" + nativeA
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		for range 200 {
			if err := r.Refresh(ctx); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for range 200 {
			for _, item := range r.List() {
				if item.Lifecycle != LifecycleActive && item.Lifecycle != LifecycleUnverified {
					t.Errorf("unexpected lifecycle %+v", item)
					return
				}
			}
			_ = r.ConditionalInput()
		}
	}()
	go func() {
		defer wg.Done()
		for range 200 {
			if _, _, err := r.Get(id); err != nil {
				t.Error(err)
				return
			}
			if _, err := r.Stream(id); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	wg.Wait()
}
