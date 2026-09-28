package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/psw7205/herdr-remote/internal/claude"
	"github.com/psw7205/herdr-remote/internal/herdr"
	"github.com/psw7205/herdr-remote/internal/stream"
	"github.com/psw7205/herdr-remote/internal/transcript"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
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
	sizes       map[string]herdr.PaneSize
	sizeErr     error
	afterSize   func()
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
func (f *fakeHerdr) PaneSize(_ context.Context, paneID string) (herdr.PaneSize, error) {
	if f.afterSize != nil {
		defer f.afterSize()
	}
	if f.sizeErr != nil {
		return herdr.PaneSize{}, f.sizeErr
	}
	size, ok := f.sizes[paneID]
	if !ok {
		return herdr.PaneSize{}, errors.New("pane missing from layout")
	}
	return size, nil
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

func TestTerminalFrameCarriesPaneSizeAndSurvivesUnknownSize(t *testing.T) {
	item := &Item{meta: Meta{ID: "claude:a", PaneID: "w1:p2", Binding: "A", Terminal: true, Active: true}}
	f := &fakeHerdr{bindings: map[string]herdr.Binding{"w1:p2": {Token: "A", TerminalID: "term-a", NativeSessionID: "a", ProcessID: 12, Agent: "claude"}}, sizes: map[string]herdr.PaneSize{"w1:p1": {Cols: 80, Rows: 24}, "w1:p2": {Cols: 161, Rows: 45}}}
	r := &Registry{gateway: f, items: map[string]*Item{item.meta.ID: item}}
	frame, err := r.Terminal(context.Background(), item.meta.ID, "A")
	if err != nil || frame.Cols != 161 || frame.Rows != 45 {
		t.Fatalf("size not attached: %+v %v", frame, err)
	}
	f.sizeErr = errors.New("layout unavailable")
	frame, err = r.Terminal(context.Background(), item.meta.ID, "A")
	if err != nil || frame.Cols != 0 || frame.Rows != 0 || frame.PaneID != "w1:p2" {
		t.Fatalf("unknown size broke frame: %+v %v", frame, err)
	}
}

func TestTerminalRejectsBindingChangedAfterLayoutRead(t *testing.T) {
	item := &Item{meta: Meta{ID: "claude:a", PaneID: "w1:p2", Binding: "A", Terminal: true, Active: true}}
	f := &fakeHerdr{bindings: map[string]herdr.Binding{"w1:p2": {Token: "A", TerminalID: "term-a", NativeSessionID: "a", ProcessID: 12, Agent: "claude"}}, sizes: map[string]herdr.PaneSize{"w1:p2": {Cols: 161, Rows: 45}}}
	f.afterSize = func() {
		f.bindings["w1:p2"] = herdr.Binding{Token: "B", TerminalID: "term-b", NativeSessionID: "b", ProcessID: 22, Agent: "claude"}
	}
	r := &Registry{gateway: f, items: map[string]*Item{item.meta.ID: item}}
	if frame, err := r.Terminal(context.Background(), item.meta.ID, "A"); err == nil {
		t.Fatalf("frame of replaced process returned: %+v", frame)
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
				if item.Lifecycle != LifecycleUnbound || item.Binding != "" {
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

func TestPaneItemSupersededBySessionMovedToItsPane(t *testing.T) {
	r, f, _ := boundRegistry(t)
	id := "claude:" + nativeA
	p2 := herdr.Agent{PaneID: "w1:p2", TerminalID: "term_a", Agent: "claude", Status: "idle", CWD: "repo"}
	p3 := herdr.Agent{PaneID: "w1:p3", TerminalID: "term_c", Agent: "claude", Status: "idle", CWD: "repo"}
	f.agents = []herdr.Agent{p2, p3}
	f.bindings[p3.PaneID] = herdr.Binding{}
	if err := r.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	events := statusEvents(t, r, "pane:w1:p3")
	f.agents = []herdr.Agent{p3}
	f.bindings[p3.PaneID] = herdr.Binding{Token: "B", TerminalID: "term_c", NativeSessionID: nativeA, ProcessID: 13, Agent: "claude", CWD: "repo"}
	if err := r.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, meta, _ := r.Get("pane:w1:p3"); meta.Lifecycle != LifecycleSuperseded || meta.SuccessorID != id {
		t.Fatalf("pane item %+v", meta)
	}
	if e := events(); !slices.Equal(e, []string{LifecycleSuperseded + ">" + id}) {
		t.Fatalf("agent.status lifecycles %v", e)
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
	if items := r.List(); len(items) != 1 || items[0].ID != "pane:w1:p2" || items[0].Lifecycle != LifecycleUnbound {
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

func TestPaneItemSupersededByVerifiedNativeSession(t *testing.T) {
	cases := []struct {
		name    string
		binding herdr.Binding
		err     error
		want    string
	}{
		{"verified before the transcript exists", herdr.Binding{Token: "T", TerminalID: "term_a", NativeSessionID: nativeA, ProcessID: 12, Agent: "claude", CWD: "repo"}, nil, LifecycleActive},
		{"never bound", herdr.Binding{}, &herdr.APIError{Code: "binding_unavailable", Message: "no native session"}, LifecycleUnbound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			a := herdr.Agent{PaneID: "w1:p2", TerminalID: "term_a", Agent: "claude", Status: "idle", CWD: "repo"}
			f := &fakeHerdr{agents: []herdr.Agent{a}, bindings: map[string]herdr.Binding{a.PaneID: tc.binding}, bindingErr: tc.err}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			r := NewRegistry(ctx, f, root)
			if err := r.Refresh(ctx); err != nil {
				t.Fatal(err)
			}
			old, next := "pane:w1:p2", "claude:"+nativeA
			if items := r.List(); len(items) != 1 || items[0].ID != old || items[0].Lifecycle != tc.want {
				t.Fatalf("pane item %+v", items)
			}
			events := statusEvents(t, r, old)
			writeTranscript(t, root, "repo", nativeA)
			f.bindingErr = nil
			f.bindings[a.PaneID] = herdr.Binding{Token: "B", TerminalID: "term_a", NativeSessionID: nativeA, ProcessID: 12, Agent: "claude", CWD: "repo"}
			if err := r.Refresh(ctx); err != nil {
				t.Fatal(err)
			}
			if items := r.List(); len(items) != 1 || items[0].ID != next || items[0].Lifecycle != LifecycleActive || items[0].Binding != "B" || !items[0].Chat {
				t.Fatalf("successor %+v", items)
			}
			_, meta, err := r.Get(old)
			if err != nil || meta.Active || meta.Lifecycle != LifecycleSuperseded || meta.SuccessorID != next || meta.Binding != "" || meta.Status != "idle" {
				t.Fatalf("pane item not superseded: %+v %v", meta, err)
			}
			if e := events(); !slices.Equal(e, []string{LifecycleSuperseded + ">" + next}) {
				t.Fatalf("agent.status lifecycles %v", e)
			}
			assertTokenRejected(t, r, old, "T", "B", "")
			assertTokenRejected(t, r, next, "T", "")
			if err := r.BoundInput(ctx, next, "B", "terminal_input", "x"); err != nil {
				t.Fatalf("successor binding rejected: %v", err)
			}
			if f.inputs != 1 {
				t.Fatalf("Herdr received %d writes", f.inputs)
			}
		})
	}
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
	// Ended items get no continuity, so the pane is re-listed as unbound.
	f.agents = []herdr.Agent{a}
	f.bindingErr = fmt.Errorf("%w: agent.binding", herdr.ErrUnsupported)
	if err := r.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if items := r.List(); len(items) != 1 || items[0].ID != "pane:w1:p2" || items[0].Lifecycle != LifecycleUnbound {
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
	if _, meta, _ := r.Get("pane:w1:p2"); meta.Lifecycle != LifecycleSuperseded || meta.SuccessorID != id {
		t.Fatalf("pane item %+v", meta)
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
	if items := r.List(); len(items) != 1 || items[0].ID != "pane:w1:p2" || items[0].Lifecycle != LifecycleUnbound {
		t.Fatalf("listed %+v", items)
	}
	for _, native := range []string{nativeA, nativeB} {
		if _, meta, _ := r.Get("claude:" + native); meta.Active || meta.Lifecycle != LifecycleEnded {
			t.Fatalf("ambiguous item kept: %+v", meta)
		}
	}
}

// `claude --resume X` on pane B while X still runs on pane A: one Refresh
// verifies X on two panes, so neither observation may claim claude:X.
func TestSameNativeVerifiedOnTwoPanesIsAmbiguous(t *testing.T) {
	p2 := herdr.Agent{PaneID: "w1:p2", TerminalID: "term_a", Agent: "claude", Status: "idle", CWD: "repo"}
	p3 := herdr.Agent{PaneID: "w1:p3", TerminalID: "term_c", Agent: "claude", Status: "idle", CWD: "repo"}
	for name, agents := range map[string][]herdr.Agent{"known pane first": {p2, p3}, "known pane last": {p3, p2}} {
		t.Run(name, func(t *testing.T) {
			r, f, _ := boundRegistry(t)
			id := "claude:" + nativeA
			f.agents = []herdr.Agent{p2, p3}
			f.bindings[p3.PaneID] = herdr.Binding{}
			if err := r.Refresh(context.Background()); err != nil {
				t.Fatal(err)
			}
			events := statusEvents(t, r, id)
			paneEvents := statusEvents(t, r, "pane:w1:p3")
			f.agents = agents
			f.bindings[p2.PaneID] = herdr.Binding{Token: "A2", TerminalID: "term_a", NativeSessionID: nativeA, ProcessID: 12, Agent: "claude", CWD: "repo"}
			f.bindings[p3.PaneID] = herdr.Binding{Token: "B", TerminalID: "term_c", NativeSessionID: nativeA, ProcessID: 13, Agent: "claude", CWD: "repo"}
			if err := r.Refresh(context.Background()); err != nil {
				t.Fatal(err)
			}
			_, meta, err := r.Get(id)
			if err != nil || !meta.Active || meta.Lifecycle != LifecycleUnverified || meta.PaneID != p2.PaneID || meta.Binding != "" || meta.Terminal {
				t.Fatalf("ambiguous native session not fail closed: %+v %v", meta, err)
			}
			_, pane, err := r.Get("pane:w1:p3")
			if err != nil || !pane.Active || pane.Lifecycle != LifecycleUnbound || pane.SuccessorID != "" || pane.Binding != "" {
				t.Fatalf("pane item superseded or bound: %+v %v", pane, err)
			}
			if e := events(); !slices.Equal(e, []string{LifecycleUnverified}) {
				t.Fatalf("agent.status lifecycles %v", e)
			}
			if e := paneEvents(); len(e) != 0 {
				t.Fatalf("pane agent.status lifecycles %v", e)
			}
			assertTokenRejected(t, r, id, "A", "A2", "B", "")
			assertTokenRejected(t, r, "pane:w1:p3", "B", "")
			if f.inputs != 0 {
				t.Fatalf("Herdr received %d writes", f.inputs)
			}
		})
	}
	t.Run("new native session", func(t *testing.T) {
		root := t.TempDir()
		writeTranscript(t, root, "repo", nativeA)
		f := &fakeHerdr{agents: []herdr.Agent{p2, p3}, bindings: map[string]herdr.Binding{
			p2.PaneID: {Token: "A", TerminalID: "term_a", NativeSessionID: nativeA, ProcessID: 12, Agent: "claude", CWD: "repo"},
			p3.PaneID: {Token: "B", TerminalID: "term_c", NativeSessionID: nativeA, ProcessID: 13, Agent: "claude", CWD: "repo"},
		}}
		r := NewRegistry(context.Background(), f, root)
		if err := r.Refresh(context.Background()); err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, item := range r.List() {
			ids = append(ids, item.ID)
			if item.Lifecycle != LifecycleUnbound || item.Binding != "" {
				t.Fatalf("ambiguous pane item not fail closed: %+v", item)
			}
		}
		if !slices.Equal(ids, []string{"pane:w1:p2", "pane:w1:p3"}) {
			t.Fatalf("listed %v", ids)
		}
		if _, _, err := r.Get("claude:" + nativeA); err == nil {
			t.Fatal("ambiguous native session claimed")
		}
	})
}

// sequenceHerdr answers successive agent.binding calls for one pane from a
// queue, so one snapshot can list a pane twice with different bindings.
type sequenceHerdr struct {
	fakeHerdr
	queue []herdr.Binding
}

func (f *sequenceHerdr) Binding(ctx context.Context, p string) (herdr.Binding, error) {
	if len(f.queue) == 0 {
		return f.fakeHerdr.Binding(ctx, p)
	}
	next := f.queue[0]
	f.queue = f.queue[1:]
	return next, nil
}

// A pane listed twice with two verified native sessions has no single
// successor, so its pane: item ends instead of picking one.
func TestDuplicatedPaneSupersedesNothing(t *testing.T) {
	root := t.TempDir()
	writeTranscript(t, root, "repo", nativeA)
	writeTranscript(t, root, "repo", nativeB)
	a := herdr.Agent{PaneID: "w1:p2", TerminalID: "term_a", Agent: "claude", Status: "idle", CWD: "repo"}
	b := a
	b.TerminalID = "term_b"
	f := &sequenceHerdr{fakeHerdr: fakeHerdr{agents: []herdr.Agent{a}, bindingErr: fmt.Errorf("%w: agent.binding", herdr.ErrUnsupported)}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := NewRegistry(ctx, f, root)
	if err := r.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	events := statusEvents(t, r, "pane:w1:p2")
	f.agents = []herdr.Agent{a, b}
	f.bindingErr = nil
	f.queue = []herdr.Binding{
		{Token: "A", TerminalID: "term_a", NativeSessionID: nativeA, ProcessID: 12, Agent: "claude", CWD: "repo"},
		{Token: "B", TerminalID: "term_b", NativeSessionID: nativeB, ProcessID: 13, Agent: "claude", CWD: "repo"},
	}
	if err := r.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if _, meta, _ := r.Get("pane:w1:p2"); meta.Active || meta.Lifecycle != LifecycleEnded || meta.SuccessorID != "" {
		t.Fatalf("duplicated pane superseded: %+v", meta)
	}
	if e := events(); !slices.Equal(e, []string{LifecycleEnded}) {
		t.Fatalf("agent.status lifecycles %v", e)
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

func TestListPreviewsNewestVisibleMessage(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "projects", "repo")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	var data []byte
	for _, line := range []map[string]any{
		{"type": "user", "uuid": "u1", "parentUuid": nil, "sessionId": nativeA, "timestamp": "2026-09-22T00:00:01.000Z", "message": map[string]any{"role": "user", "content": "question"}},
		{"type": "assistant", "uuid": "a1", "parentUuid": "u1", "sessionId": nativeA, "timestamp": "2026-09-22T00:00:02.000Z", "message": map[string]any{"role": "assistant", "content": []map[string]string{{"type": "text", "text": "first line\n\n  second\tline " + strings.Repeat("가", 200)}}}},
		{"type": "user", "uuid": "c1", "parentUuid": "a1", "sessionId": nativeA, "timestamp": "2026-09-22T00:00:03.000Z", "message": map[string]any{"role": "user", "content": "<command-name>/sample</command-name>"}},
	} {
		b, err := json.Marshal(line)
		if err != nil {
			t.Fatal(err)
		}
		data = append(append(data, b...), '\n')
	}
	if err := os.WriteFile(filepath.Join(dir, nativeA+".jsonl"), data, 0600); err != nil {
		t.Fatal(err)
	}
	a := herdr.Agent{PaneID: "w1:p2", TerminalID: "term_a", Agent: "claude", Status: "idle"}
	f := &fakeHerdr{agents: []herdr.Agent{a}, bindings: map[string]herdr.Binding{a.PaneID: {Token: "A", TerminalID: a.TerminalID, NativeSessionID: nativeA, ProcessID: 12, Agent: "claude", CWD: "repo"}}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := NewRegistry(ctx, f, root)
	if err := r.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(2 * time.Second)
	for r.List()[0].LastMessage == nil {
		select {
		case <-deadline:
			t.Fatal("preview not loaded")
		case <-time.After(10 * time.Millisecond):
		}
	}
	// The newest record is a hidden slash command, so the assistant reply is the newest visible message.
	want := MessagePreview{Role: "assistant", Text: "first line second line " + strings.Repeat("가", 136) + "…"}
	item := r.List()[0]
	if item.LastActivity != "2026-09-22T00:00:02.000Z" || item.LastMessage == nil || *item.LastMessage != want || len([]rune(want.Text)) != 160 {
		t.Fatalf("wrong preview: %q %+v", item.LastActivity, item.LastMessage)
	}
	if _, meta, err := r.Get(item.ID); err != nil || meta.LastActivity != item.LastActivity || meta.LastMessage == nil || *meta.LastMessage != want {
		t.Fatalf("Get differs from List: %+v %v", meta, err)
	}
	b, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(b, &body); err != nil {
		t.Fatal(err)
	}
	if body["last_activity"] != "2026-09-22T00:00:02.000Z" || !reflect.DeepEqual(body["last_message"], map[string]any{"role": "assistant", "text": want.Text}) {
		t.Fatalf("wrong JSON contract: %s", b)
	}
}

func TestListOmitsPreviewWithoutValidChat(t *testing.T) {
	item := &Item{nativeID: nativeA, meta: Meta{ID: "claude:" + nativeA, PaneID: "w1:p2", Chat: true, Active: true, Status: "idle"}, projection: claude.NewProjection(), stream: stream.New("claude:" + nativeA)}
	noChat := &Item{meta: Meta{ID: "pane:w1:p3", PaneID: "w1:p3", Active: true, Status: "idle"}, last: []Message{{ID: "stale", Role: "user", Text: "stale", Timestamp: "2026-09-22T00:00:01.000Z"}}}
	registry := &Registry{gateway: &fakeHerdr{}, items: map[string]*Item{item.meta.ID: item, noChat.meta.ID: noChat}}
	valid := []byte(`{"type":"user","uuid":"u1","parentUuid":null,"sessionId":"` + nativeA + `","timestamp":"2026-09-22T00:00:01.000Z","message":{"role":"user","content":"hello"}}`)
	if err := item.apply(transcript.Batch{Reset: true, Lines: [][]byte{valid}}); err != nil {
		t.Fatal(err)
	}
	if _, meta, _ := registry.Get(item.meta.ID); meta.LastMessage == nil || *meta.LastMessage != (MessagePreview{Role: "user", Text: "hello"}) {
		t.Fatalf("valid chat lacks preview: %+v", meta)
	}
	wrong := []byte(`{"type":"user","uuid":"u2","parentUuid":"u1","sessionId":"` + nativeB + `","message":{"role":"user","content":"other session"}}`)
	if err := item.apply(transcript.Batch{Lines: [][]byte{wrong}}); err == nil {
		t.Fatal("wrong transcript accepted")
	}
	for _, meta := range registry.List() {
		b, err := json.Marshal(meta)
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		if err := json.Unmarshal(b, &body); err != nil {
			t.Fatal(err)
		}
		_, activity := body["last_activity"]
		_, message := body["last_message"]
		if activity || message {
			t.Fatalf("preview published without valid chat: %s", b)
		}
	}
}

func TestPreviewTextCollapsesWhitespaceAndCutsRunes(t *testing.T) {
	exact := strings.Repeat("가", 160)
	for in, want := range map[string]string{
		" \n a\t\tb \r\n c ": "a b c",
		" \n\t ":             "",
		exact:                exact,
		exact + "나":          strings.Repeat("가", 159) + "…",
		"x\n" + exact:        "x " + strings.Repeat("가", 157) + "…",
	} {
		if got := previewText(in); got != want {
			t.Fatalf("previewText(%q) = %q, want %q", in, got, want)
		}
	}
}

// watcherOf returns the item's watcher done channel, nil when none runs.
func watcherOf(r *Registry, id string) chan struct{} {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.items[id].done
}

func appendUserLine(t *testing.T, root, uuid, parent string) {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(root, "projects", "repo", nativeA+".jsonl"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	line := `{"type":"user","uuid":"` + uuid + `","parentUuid":"` + parent + `","sessionId":"` + nativeA + `","message":{"role":"user","content":"sample"}}` + "\n"
	if _, err := f.WriteString(line); err != nil {
		t.Fatal(err)
	}
}

func waitHistory(t *testing.T, r *Registry, id, uuid string) stream.Snapshot {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		snap, _, err := r.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(snap.Data), `"`+uuid+`"`) {
			return snap
		}
		select {
		case <-deadline:
			t.Fatalf("%s not loaded: %s", uuid, snap.Data)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func waitClosed(t *testing.T, done chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("transcript watcher still running")
	}
}

// The watcher, and with it the fsnotify watcher on the transcript directory,
// lives only while the item is active. Unverified keeps it; ended releases it;
// a revive re-reads the transcript under a new epoch without duplicates.
func TestTranscriptWatcherFollowsItemLifecycle(t *testing.T) {
	r, f, root := boundRegistry(t)
	id := "claude:" + nativeA
	first := watcherOf(r, id)
	if first == nil {
		t.Fatal("active item has no watcher")
	}
	a := f.agents[0]
	a.TerminalID = "term_b"

	// Unverified: the agent still writes, so Chat keeps updating on the same watcher.
	f.agents = []herdr.Agent{a}
	f.bindingErr = fmt.Errorf("%w: agent.binding", herdr.ErrUnsupported)
	if err := r.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, meta, _ := r.Get(id); meta.Lifecycle != LifecycleUnverified || watcherOf(r, id) != first {
		t.Fatalf("unverified item lost its watcher: %+v", meta)
	}
	appendUserLine(t, root, "u2", "u-"+nativeA)
	epoch := waitHistory(t, r, id, "u2").Cursor.Epoch

	// Ended: the watcher exits and later appends are not read.
	f.agents = nil
	if err := r.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if watcherOf(r, id) != nil {
		t.Fatal("ended item kept its watcher")
	}
	waitClosed(t, first)
	before, _, _ := r.Get(id)
	appendUserLine(t, root, "u3", "u2")
	if after, _, _ := r.Get(id); after.Cursor != before.Cursor || string(after.Data) != string(before.Data) {
		t.Fatalf("ended item changed: %+v -> %+v", before.Cursor, after.Cursor)
	}

	// Unbound pane: item, then revive: a new watcher resyncs under a new epoch
	// and the pane: item is superseded without ever owning a watcher.
	f.agents = []herdr.Agent{a}
	if err := r.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if watcherOf(r, "pane:w1:p2") != nil {
		t.Fatal("pane: item without Chat has a watcher")
	}
	f.bindingErr = nil
	f.bindings[a.PaneID] = herdr.Binding{Token: "C", TerminalID: "term_b", NativeSessionID: nativeA, ProcessID: 14, Agent: "claude", CWD: "repo"}
	if err := r.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, meta, _ := r.Get("pane:w1:p2"); meta.Lifecycle != LifecycleSuperseded || watcherOf(r, "pane:w1:p2") != nil {
		t.Fatalf("superseded pane item %+v", meta)
	}
	second := watcherOf(r, id)
	if second == nil || second == first {
		t.Fatal("revived item has no new watcher")
	}
	snap := waitHistory(t, r, id, "u3")
	if snap.Cursor.Epoch == epoch {
		t.Fatal("revive resync kept the old epoch")
	}
	var messages []Message
	if err := json.Unmarshal(snap.Data, &messages); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, m := range messages {
		ids = append(ids, m.ID)
	}
	if !slices.Equal(ids, []string{"u-" + nativeA, "u2", "u3"}) {
		t.Fatalf("revived history %v", ids)
	}

	// Ending again releases the restarted watcher too.
	f.agents = nil
	if err := r.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitClosed(t, second)
}

// A cancelled watcher that is still finishing a read must not publish into the
// item, which may already belong to its successor.
func TestCancelledWatcherPublishesNothing(t *testing.T) {
	root := t.TempDir()
	writeTranscript(t, root, "repo", nativeA)
	path, err := claude.Resolve(root, nativeA)
	if err != nil {
		t.Fatal(err)
	}
	item := &Item{nativeID: nativeA, path: path, meta: Meta{ID: "claude:" + nativeA, Chat: true, Active: true}, projection: claude.NewProjection(), stream: stream.New("claude:" + nativeA)}
	before := item.stream.Snapshot()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	item.watch(ctx)
	if after := item.stream.Snapshot(); after.Cursor != before.Cursor || string(after.Data) != string(before.Data) || item.invalid || item.last != nil {
		t.Fatalf("cancelled watcher published: %+v %s", after.Cursor, after.Data)
	}
}
