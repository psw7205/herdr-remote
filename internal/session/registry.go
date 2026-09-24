package session

import (
	"context"
	"errors"
	"fmt"
	"herdr-remote/internal/claude"
	"herdr-remote/internal/herdr"
	"herdr-remote/internal/stream"
	"herdr-remote/internal/transcript"
	"log/slog"
	"slices"
	"strings"
	"sync"
)

type Gateway interface {
	Snapshot(context.Context) (herdr.Snapshot, error)
	Binding(context.Context, string) (herdr.Binding, error)
	BoundInput(context.Context, string, string, string, string) error
	TerminalSnapshot(context.Context, string) (herdr.TerminalSnapshot, error)
}
type Message struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Text      string `json:"text"`
	Timestamp string `json:"timestamp"`
}
type Meta struct {
	ID       string `json:"id"`
	Agent    string `json:"agent"`
	PaneID   string `json:"pane_id"`
	Project  string `json:"project"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Binding  string `json:"runtime_binding,omitempty"`
	Chat     bool   `json:"chat"`
	Terminal bool   `json:"terminal"`
	Active   bool   `json:"active"`
	// Lifecycle is derived from ID, Active, Binding and SuccessorID; see lifecycle.
	Lifecycle string `json:"lifecycle"`
	// SuccessorID names the claude:<native> item that replaced this pane: item.
	SuccessorID string `json:"successor_id,omitempty"`
}
type Item struct {
	meta       Meta
	stream     *stream.Session
	path       string
	nativeID   string
	mu         sync.Mutex
	projection *claude.Projection
	last       []Message
	lastError  string
	loading    bool
	invalid    bool
}
type Registry struct {
	mu         sync.RWMutex
	ctx        context.Context
	gateway    Gateway
	claudeRoot string
	items      map[string]*Item
	// conditional is the agent.binding capability seen by the last successful Refresh.
	conditional string
}

func NewRegistry(ctx context.Context, gateway Gateway, claudeRoot string) *Registry {
	return &Registry{ctx: ctx, gateway: gateway, claudeRoot: claudeRoot, items: make(map[string]*Item), conditional: herdr.ConditionalInputUnknown}
}
func (r *Registry) ConditionalInput() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.conditional
}
func status(raw string) string {
	switch raw {
	case "idle":
		return "idle"
	case "done":
		return "completed"
	case "working":
		return "working"
	case "blocked":
		return "needs_attention"
	default:
		return "error"
	}
}

// Lifecycle values published in Meta and agent.status.
//
//   - active: Herdr verified the current runtime binding.
//   - unverified: a claude:<native> item (created only from a verified binding)
//     lost its binding while Herdr still runs claude on its pane; every write
//     fails closed until the same native session is verified again.
//   - unbound: a pane:<pane> item, whose native session was never identified,
//     has no verified binding (stock Herdr, or no binding yet); writes fail closed.
//   - superseded: a pane:<pane> item whose pane gained a verified claude:<native>
//     item in the same Refresh; SuccessorID names it. The agent keeps running.
//   - ended: the pane left the Herdr snapshot, Herdr no longer reports claude on
//     it, or the pane provably hosts another native session.
const (
	LifecycleActive     = "active"
	LifecycleUnverified = "unverified"
	LifecycleUnbound    = "unbound"
	LifecycleSuperseded = "superseded"
	LifecycleEnded      = "ended"
)

func lifecycle(meta Meta) string {
	switch {
	case !meta.Active && meta.SuccessorID != "":
		return LifecycleSuperseded
	case !meta.Active:
		return LifecycleEnded
	case meta.Binding == "" && strings.HasPrefix(meta.ID, "claude:"):
		return LifecycleUnverified
	case meta.Binding == "":
		return LifecycleUnbound
	default:
		return LifecycleActive
	}
}

type observation struct {
	agent    herdr.Agent
	binding  herdr.Binding
	verified bool
	path     string
	resolved bool
	// boundNative is the native session a successful agent.binding call named,
	// even when the binding was rejected (e.g. a terminal_id race).
	boundNative string
}

// reportedNativeID is the native session that Herdr's own agent hook reported
// for the pane (the predicate doctor uses), or "" when nothing was reported.
func reportedNativeID(a herdr.Agent) string {
	if s := a.Session; s != nil && s.Agent == "claude" && s.Kind == "id" {
		return s.Value
	}
	return ""
}

func (r *Registry) Refresh(ctx context.Context) error {
	snapshot, err := r.gateway.Snapshot(ctx)
	if err != nil {
		return err
	}
	// Gateway and filesystem calls stay outside r.mu.
	observed := make([]observation, 0, len(snapshot.Agents))
	conditional := herdr.ConditionalInputUnknown
	for _, a := range snapshot.Agents {
		if a.Agent != "claude" || a.PaneID == "" {
			continue
		}
		o := observation{agent: a}
		binding, e := r.gateway.Binding(ctx, a.PaneID)
		conditional = herdr.ObserveConditionalInput(conditional, e)
		if e == nil {
			o.boundNative = binding.NativeSessionID
		}
		if e == nil && binding.Agent == "claude" && binding.TerminalID == a.TerminalID && binding.Token != "" && binding.ProcessID != 0 && binding.NativeSessionID != "" {
			o.binding, o.verified = binding, true
			if path, e := claude.Resolve(r.claudeRoot, binding.NativeSessionID); e == nil {
				o.path, o.resolved = path, true
			}
		}
		observed = append(observed, o)
	}
	// Two panes verified as one native session in one snapshot (e.g.
	// `claude --resume X` on another pane while X still runs) cannot be told
	// apart, so none of them may claim claude:<native> with a binding. They fall
	// back to read-only continuity: a known item stays unverified on its own
	// pane, the other panes list as unbound pane: items, and none supersedes.
	claims := make(map[string]int)
	panes := make(map[string]int)
	for _, o := range observed {
		panes[o.agent.PaneID]++
		if o.verified {
			claims[o.binding.NativeSessionID]++
		}
	}
	for i := range observed {
		if o := &observed[i]; o.verified && claims[o.binding.NativeSessionID] > 1 {
			o.binding, o.verified, o.path, o.resolved = herdr.Binding{}, false, "", false
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.conditional = conditional
	// Continuity without a verified binding: an active claude:<native> item
	// stays on its pane, as "unverified", while Herdr still reports claude on
	// that pane and neither Herdr's own native session report (agent_session)
	// nor a rejected agent.binding result names another native ID. A verified
	// binding for the same native ID elsewhere, a different native report, or
	// the pane leaving the snapshot ends it.
	// terminal_id is not used because live handoff reissues it, and cwd cannot
	// tell two sessions of one repository apart. Without a native report, a
	// claude-to-claude swap in the same pane is indistinguishable: the item
	// keeps showing the old session's own read-only transcript as "unverified",
	// with writes and Terminal closed, until a verified binding confirms or
	// replaces it.
	byPane := make(map[string]*Item)
	// Each observation yields one found ID, so two active items share a pane
	// only when a snapshot listed that pane twice; no continuity then.
	ambiguous := make(map[string]bool)
	for _, item := range r.items {
		if !item.meta.Active || item.nativeID == "" {
			continue
		}
		if _, dup := byPane[item.meta.PaneID]; dup {
			ambiguous[item.meta.PaneID] = true
		}
		byPane[item.meta.PaneID] = item
	}
	found := make(map[string]Meta)
	paths := make(map[string]string)
	// Verified observations claim IDs first so that an unverified continuity
	// observation can never override a verified one, whatever the snapshot order.
	for _, o := range observed {
		if !o.verified {
			continue
		}
		a := o.agent
		meta := Meta{ID: "pane:" + a.PaneID, Agent: a.Agent, PaneID: a.PaneID, Project: o.binding.CWD, Title: a.Title, Status: status(a.Status), Active: true, Binding: o.binding.Token, Terminal: true}
		id := "claude:" + o.binding.NativeSessionID
		if item, ok := r.items[id]; ok {
			// Same native session as a known item: keep its identity even if
			// the transcript cannot be resolved again right now.
			meta.ID = id
			meta.Chat = item.path != ""
		} else if o.resolved {
			meta.ID = id
			meta.Chat = true
			paths[id] = o.path
		}
		found[meta.ID] = meta
	}
	for _, o := range observed {
		if o.verified {
			continue
		}
		a := o.agent
		meta := Meta{ID: "pane:" + a.PaneID, Agent: a.Agent, PaneID: a.PaneID, Project: a.CWD, Title: a.Title, Status: status(a.Status), Active: true}
		if item := byPane[a.PaneID]; item != nil && !ambiguous[a.PaneID] {
			_, taken := found[item.meta.ID]
			reported := reportedNativeID(a)
			if !taken && (reported == "" || reported == item.nativeID) && (o.boundNative == "" || o.boundNative == item.nativeID) {
				meta.ID = item.meta.ID
				meta.Chat = item.path != ""
				meta.Project = item.meta.Project
			}
		}
		found[meta.ID] = meta
	}
	// A pane: item is superseded, not ended, when its pane gained a verified
	// claude: item that was not already active on that pane: same pane_id,
	// newly verified there. A pane the snapshot listed twice, or shared by two
	// active items, may yield several candidates, so it supersedes nothing.
	successors := make(map[string]string)
	for id, meta := range found {
		if meta.Binding == "" || !strings.HasPrefix(id, "claude:") || ambiguous[meta.PaneID] || panes[meta.PaneID] > 1 {
			continue
		}
		if prev, ok := r.items[id]; ok && prev.meta.Active && prev.meta.PaneID == meta.PaneID {
			continue
		}
		successors[meta.PaneID] = id
	}
	for id, item := range r.items {
		if _, ok := found[id]; ok || !item.meta.Active {
			continue
		}
		item.meta.Active = false
		item.meta.Binding = ""
		item.meta.Terminal = false
		if next := successors[item.meta.PaneID]; next != "" && id == "pane:"+item.meta.PaneID {
			item.meta.SuccessorID = next
			item.meta.Status = found[next].Status
			item.stream.Append("agent.status", map[string]any{"status": item.meta.Status, "lifecycle": LifecycleSuperseded, "successor_id": next})
			continue
		}
		item.meta.Status = "completed"
		item.stream.Append("agent.status", map[string]any{"status": "completed", "lifecycle": LifecycleEnded})
	}
	for id, meta := range found {
		if item, ok := r.items[id]; ok {
			if item.meta.Binding != meta.Binding || item.meta.Status != meta.Status || item.meta.Active != meta.Active {
				item.stream.Append("agent.status", map[string]any{"status": meta.Status, "lifecycle": lifecycle(meta)})
			}
			item.meta = meta
			continue
		}
		item := &Item{meta: meta, path: paths[id], stream: stream.New(id), projection: claude.NewProjection()}
		if native, ok := strings.CutPrefix(id, "claude:"); ok {
			item.nativeID = native
		}
		r.items[id] = item
		if meta.Chat {
			go item.watch(r.ctx)
		}
	}
	return nil
}
func (item *Item) watch(ctx context.Context) {
	err := transcript.Watch(ctx, item.path, item.apply, func(err error) {
		item.mu.Lock()
		defer item.mu.Unlock()
		if item.lastError != err.Error() {
			item.invalid = true
			item.lastError = err.Error()
			item.stream.Append("session.error", map[string]string{"message": "Transcript unavailable. Open Terminal."})
			slog.Warn("transcript watcher", "error", err)
		}
	})
	if err != nil {
		slog.Error("transcript watcher stopped", "error", err)
		item.stream.Append("session.error", map[string]string{"message": "Transcript watcher unavailable. Open Terminal."})
	}
}
func (item *Item) apply(batch transcript.Batch) error {
	item.mu.Lock()
	defer item.mu.Unlock()
	if batch.Reset {
		item.projection = claude.NewProjection()
		item.last = nil
		item.loading = true
		item.invalid = false
	}
	if item.invalid {
		return errors.New("transcript identity requires resync")
	}
	for _, line := range batch.Lines {
		record, err := claude.Decode(line, item.nativeID)
		if err != nil {
			item.invalid = true
			return err
		}
		if err = item.projection.Add(record); err != nil {
			item.invalid = true
			return err
		}
	}
	if batch.More {
		return nil
	}
	nodes := item.projection.Messages()
	current := make([]Message, 0, len(nodes))
	for _, node := range nodes {
		current = append(current, Message{ID: node.ID, Role: node.Role, Text: node.Text, Timestamp: node.Timestamp})
	}
	if item.loading {
		if err := item.stream.Reset(current); err != nil {
			return err
		}
		item.last = current
		item.loading = false
		item.lastError = ""
		return nil
	}
	if slices.Equal(item.last, current) {
		return nil
	}
	if len(current) < len(item.last) || !slices.Equal(current[:min(len(current), len(item.last))], item.last) {
		item.last = current
		return item.stream.Reset(current)
	}
	changes := make([]stream.Change, 0, len(current)-len(item.last))
	for _, m := range current[len(item.last):] {
		changes = append(changes, stream.Change{Type: "message." + m.Role, Payload: m, Source: "claude.transcript"})
	}
	item.last = current
	return item.stream.Commit(current, changes)
}
func (r *Registry) List() []Meta {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Meta, 0, len(r.items))
	for _, item := range r.items {
		if item.meta.Active {
			out = append(out, effectiveMeta(item))
		}
	}
	slices.SortFunc(out, func(a, b Meta) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	return out
}

func effectiveMeta(item *Item) Meta {
	meta := item.meta
	meta.Lifecycle = lifecycle(meta)
	item.mu.Lock()
	if item.invalid {
		meta.Chat = false
	}
	item.mu.Unlock()
	return meta
}
func (r *Registry) Get(id string) (stream.Snapshot, Meta, error) {
	r.mu.RLock()
	item, ok := r.items[id]
	if !ok {
		r.mu.RUnlock()
		return stream.Snapshot{}, Meta{}, errors.New("session not found")
	}
	meta := effectiveMeta(item)
	r.mu.RUnlock()
	return item.stream.Snapshot(), meta, nil
}
func (r *Registry) Stream(id string) (*stream.Session, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	item, ok := r.items[id]
	if !ok {
		return nil, errors.New("session not found")
	}
	return item.stream, nil
}
func (r *Registry) BoundInput(ctx context.Context, id, binding, kind, text string) error {
	r.mu.RLock()
	item, ok := r.items[id]
	if !ok {
		r.mu.RUnlock()
		return errors.New("session not found")
	}
	meta := effectiveMeta(item)
	r.mu.RUnlock()
	if !meta.Active || meta.Binding == "" || binding != meta.Binding {
		return errors.New("session binding changed")
	}
	if kind == "prompt" && !meta.Chat {
		return errors.New("chat transcript unavailable")
	}
	if kind == "prompt" && meta.Status != "idle" && meta.Status != "completed" {
		return errors.New("agent is not ready for a chat prompt")
	}
	if kind == "terminal_input" && !meta.Terminal {
		return errors.New("terminal unavailable")
	}
	return r.gateway.BoundInput(ctx, meta.PaneID, binding, kind, text)
}
func (r *Registry) Terminal(ctx context.Context, id, binding string) (herdr.TerminalSnapshot, error) {
	r.mu.RLock()
	item, ok := r.items[id]
	if !ok {
		r.mu.RUnlock()
		return herdr.TerminalSnapshot{}, errors.New("session not found")
	}
	meta := item.meta
	r.mu.RUnlock()
	if !meta.Active || !meta.Terminal || binding == "" || binding != meta.Binding {
		return herdr.TerminalSnapshot{}, errors.New("session binding changed")
	}
	current, err := r.gateway.Binding(ctx, meta.PaneID)
	if err != nil || current.Token != binding {
		return herdr.TerminalSnapshot{}, errors.New("session binding changed")
	}
	frame, err := r.gateway.TerminalSnapshot(ctx, meta.PaneID)
	if err != nil {
		return frame, err
	}
	if frame.PaneID != meta.PaneID {
		return herdr.TerminalSnapshot{}, fmt.Errorf("terminal target changed")
	}
	current, err = r.gateway.Binding(ctx, meta.PaneID)
	if err != nil || current.Token != binding {
		return herdr.TerminalSnapshot{}, errors.New("session binding changed")
	}
	return frame, nil
}
