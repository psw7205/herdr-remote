package session

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"github.com/psw7205/herdr-remote/internal/claude"
	"github.com/psw7205/herdr-remote/internal/codex"
	"github.com/psw7205/herdr-remote/internal/herdr"
	"github.com/psw7205/herdr-remote/internal/stream"
	"github.com/psw7205/herdr-remote/internal/transcript"
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
	PaneSize(context.Context, string) (herdr.PaneSize, error)
}

// Message is one Chat item. Role "tool" items carry Tool and an empty Text;
// user and assistant messages never carry Tool.
type Message struct {
	ID        string       `json:"id"`
	Role      string       `json:"role"`
	Text      string       `json:"text"`
	Timestamp string       `json:"timestamp"`
	Tool      ToolActivity `json:"tool,omitzero"`
}

// ToolActivity is one tool call: State is running, completed, error or
// unknown. Input (pretty JSON) and Result are capped display text; the
// Truncated flags say whether a cap cut them.
type ToolActivity struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Summary         string `json:"summary"`
	State           string `json:"state"`
	Input           string `json:"input"`
	InputTruncated  bool   `json:"input_truncated"`
	Result          string `json:"result"`
	ResultTruncated bool   `json:"result_truncated"`
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
	// SuccessorID names the claude:<native> or codex:<native> item that
	// replaced this pane: item.
	SuccessorID string `json:"successor_id,omitempty"`
	// LastActivity and LastMessage describe the newest visible Chat message;
	// both are omitted without a valid Chat history.
	LastActivity string          `json:"last_activity,omitempty"`
	LastMessage  *MessagePreview `json:"last_message,omitempty"`
}

// MessagePreview is a single-line excerpt of at most previewRunes runes.
type MessagePreview struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

const previewRunes = 160

func previewText(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) <= previewRunes {
		return text
	}
	return string(runes[:previewRunes-1]) + "…"
}

// projection turns decoded records into the visible Chat history.
type projection interface {
	Add(transcript.Record) error
	Messages() []transcript.Record
}

// adapter reads one agent's native transcript. The item ID prefix names the
// native session it reads; no adapter writes to the agent.
type adapter struct {
	prefix        string
	source        string
	resolve       func(root, id string) (string, error)
	decode        func(line []byte, sessionID string) (transcript.Record, error)
	newProjection func() projection
}

var (
	claudeAdapter = &adapter{prefix: "claude:", source: "claude.transcript", resolve: claude.Resolve, decode: claude.Decode, newProjection: func() projection { return claude.NewProjection() }}
	codexAdapter  = &adapter{prefix: "codex:", source: "codex.transcript", resolve: codex.Resolve, decode: codex.Decode, newProjection: func() projection { return codex.NewProjection() }}
)

func adapterFor(id string) *adapter {
	if strings.HasPrefix(id, codexAdapter.prefix) {
		return codexAdapter
	}
	return claudeAdapter
}

type Item struct {
	meta     Meta
	stream   *stream.Session
	path     string
	nativeID string
	// format is set when the item is created and never changes, so the
	// watcher reads it without Registry.mu. Nil means Claude.
	format     *adapter
	mu         sync.Mutex
	projection projection
	last       []Message
	lastError  string
	loading    bool
	invalid    bool
	// cancel stops the transcript watcher and done closes when it has
	// returned; both are nil while no watcher runs. Guarded by Registry.mu.
	cancel context.CancelFunc
	done   chan struct{}
	// ended orders inactive items for eviction; guarded by Registry.mu.
	ended uint64
}

func (item *Item) adapter() *adapter {
	if item.format == nil {
		return claudeAdapter
	}
	return item.format
}

type Registry struct {
	mu         sync.RWMutex
	ctx        context.Context
	gateway    Gateway
	claudeRoot string
	codexRoot  string
	items      map[string]*Item
	// conditional is the agent.binding capability seen by the last successful Refresh.
	conditional string
	ends        uint64
}

// maxInactive bounds the ended and superseded items kept for clients still
// viewing them. Older ones are evicted; a native session seen again gets a new
// item whose fresh epoch makes clients resync from the snapshot.
const maxInactive = 32

type Option func(*Registry)

// WithCodexRoot sets the Codex home whose sessions/ holds rollouts. Without
// it, Codex panes are listed but their Chat is unavailable.
func WithCodexRoot(root string) Option { return func(r *Registry) { r.codexRoot = root } }

func NewRegistry(ctx context.Context, gateway Gateway, claudeRoot string, options ...Option) *Registry {
	r := &Registry{ctx: ctx, gateway: gateway, claudeRoot: claudeRoot, items: make(map[string]*Item), conditional: herdr.ConditionalInputUnknown}
	for _, option := range options {
		option(r)
	}
	return r
}

func (r *Registry) resolve(a *adapter, native string) (string, error) {
	if a != codexAdapter {
		return a.resolve(r.claudeRoot, native)
	}
	if r.codexRoot == "" {
		return "", errors.New("Codex root not configured")
	}
	return a.resolve(r.codexRoot, native)
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
//   - unbound: the item has no verified binding and never had one, so every
//     write fails closed: a pane:<pane> item whose native session was never
//     identified (stock Herdr, or no binding yet), or any codex:<native> item,
//     which is read-only because Herdr issues no binding for Codex.
//   - superseded: a pane:<pane> item whose pane gained a verified claude:<native>
//     item, or a codex:<native> item from Herdr's agent_session report, in the
//     same Refresh; SuccessorID names it. The agent keeps running.
//   - ended: the pane left the Herdr snapshot, Herdr no longer reports that
//     agent on it, or the pane provably hosts another native session.
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
	if s := a.Session; s != nil && s.Agent == a.Agent && s.Kind == "id" {
		return s.Value
	}
	return ""
}

// codexObservation is a Codex pane. Its only identity is Herdr's agent_session
// report: Herdr issues no runtime binding for Codex, so the item is read-only.
type codexObservation struct {
	agent    herdr.Agent
	native   string
	path     string
	resolved bool
}

func (r *Registry) Refresh(ctx context.Context) error {
	snapshot, err := r.gateway.Snapshot(ctx)
	if err != nil {
		return err
	}
	// Gateway and filesystem calls stay outside r.mu.
	observed := make([]observation, 0, len(snapshot.Agents))
	var codexObserved []codexObservation
	codexClaims := make(map[string]int)
	conditional := herdr.ConditionalInputUnknown
	for _, a := range snapshot.Agents {
		// Codex panes never reach agent.binding: Herdr verifies no Codex
		// binding, and an error for one must not move the server-wide
		// conditional input capability that Claude writes depend on.
		if a.Agent == "codex" && a.PaneID != "" {
			o := codexObservation{agent: a, native: reportedNativeID(a)}
			if o.native != "" {
				codexClaims[o.native]++
			}
			codexObserved = append(codexObserved, o)
			continue
		}
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
	// Two Codex panes reporting one native session (e.g. `codex resume X`
	// beside a running X) claim nothing, like two Claude panes verified as one.
	for i := range codexObserved {
		o := &codexObserved[i]
		panes[o.agent.PaneID]++
		if o.native == "" || codexClaims[o.native] > 1 {
			continue
		}
		if path, e := r.resolve(codexAdapter, o.native); e == nil {
			o.path, o.resolved = path, true
		}
	}
	// Claude Code moves a session's transcript to another project directory
	// when the session changes its working directory (e.g. into a worktree).
	// Every active native item is resolved again by its native ID so that a
	// moved transcript is followed; an unresolvable ID keeps the old path.
	// Keys are item IDs, so the two agents' native IDs never meet.
	resolved := make(map[string]string)
	for _, o := range observed {
		if o.resolved {
			resolved[claudeAdapter.prefix+o.binding.NativeSessionID] = o.path
		}
	}
	for _, o := range codexObserved {
		if o.resolved {
			resolved[codexAdapter.prefix+o.native] = o.path
		}
	}
	r.mu.RLock()
	var pending []*Item
	for id, item := range r.items {
		if _, ok := resolved[id]; item.meta.Active && item.path != "" && !ok {
			pending = append(pending, item)
		}
	}
	r.mu.RUnlock()
	for _, item := range pending {
		// format and nativeID never change after the item is created.
		if path, e := r.resolve(item.adapter(), item.nativeID); e == nil {
			resolved[item.adapter().prefix+item.nativeID] = path
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
	// A Codex item is the native session Herdr's agent_session reported for
	// the pane. It never carries a binding or Terminal, so it is read-only.
	claimedCodex := make(map[int]bool)
	for i, o := range codexObserved {
		if o.native == "" || codexClaims[o.native] > 1 {
			continue
		}
		a := o.agent
		id := codexAdapter.prefix + o.native
		meta := Meta{ID: id, Agent: a.Agent, PaneID: a.PaneID, Project: a.CWD, Title: a.Title, Status: status(a.Status), Active: true}
		if item, ok := r.items[id]; ok {
			meta.Chat = item.path != ""
		} else if o.resolved {
			meta.Chat = true
			paths[id] = o.path
		} else {
			continue
		}
		claimedCodex[i] = true
		found[id] = meta
	}
	for _, o := range observed {
		if o.verified {
			continue
		}
		a := o.agent
		meta := Meta{ID: "pane:" + a.PaneID, Agent: a.Agent, PaneID: a.PaneID, Project: a.CWD, Title: a.Title, Status: status(a.Status), Active: true}
		if item := byPane[a.PaneID]; item != nil && !ambiguous[a.PaneID] && item.adapter() == claudeAdapter {
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
	// A Codex pane without a claim keeps the codex: item already on it while
	// Herdr reports no session there or still reports that one; otherwise it
	// lists as a status-only pane: item.
	for i, o := range codexObserved {
		if claimedCodex[i] {
			continue
		}
		a := o.agent
		meta := Meta{ID: "pane:" + a.PaneID, Agent: a.Agent, PaneID: a.PaneID, Project: a.CWD, Title: a.Title, Status: status(a.Status), Active: true}
		if item := byPane[a.PaneID]; item != nil && !ambiguous[a.PaneID] && item.adapter() == codexAdapter {
			if _, taken := found[item.meta.ID]; !taken && (o.native == "" || o.native == item.nativeID) {
				meta.ID = item.meta.ID
				meta.Chat = item.path != ""
				meta.Project = item.meta.Project
			}
		}
		found[meta.ID] = meta
	}
	// A pane: item is superseded, not ended, when its pane gained a verified
	// claude: item, or a reported codex: item, that was not already active on
	// that pane: same pane_id, newly identified there. A pane the snapshot
	// listed twice, or shared by two active items, may yield several
	// candidates, so it supersedes nothing.
	successors := make(map[string]string)
	for id, meta := range found {
		identified := meta.Binding != "" && strings.HasPrefix(id, claudeAdapter.prefix) || strings.HasPrefix(id, codexAdapter.prefix)
		if !identified || ambiguous[meta.PaneID] || panes[meta.PaneID] > 1 {
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
		item.stopWatch()
		item.release()
		r.ends++
		item.ended = r.ends
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
			if path, ok := resolved[id]; ok && item.path != "" && path != item.path {
				slog.Info("transcript moved", "session", id)
				item.stopWatch()
				item.path = path
			}
			// A revived or moved item re-reads its transcript from the start, so
			// the first batch resets the projection and replaces the stream epoch.
			if meta.Chat && item.cancel == nil {
				item.startWatch(r.ctx)
			}
			continue
		}
		format := adapterFor(id)
		item := &Item{meta: meta, path: paths[id], stream: stream.New(id), format: format, projection: format.newProjection()}
		if native, ok := strings.CutPrefix(id, format.prefix); ok {
			item.nativeID = native
		}
		r.items[id] = item
		if meta.Chat {
			item.startWatch(r.ctx)
		}
	}
	r.evictInactive()
	return nil
}

// evictInactive runs under Registry.mu.
func (r *Registry) evictInactive() {
	var inactive []*Item
	for _, item := range r.items {
		if !item.meta.Active {
			inactive = append(inactive, item)
		}
	}
	if len(inactive) <= maxInactive {
		return
	}
	slices.SortFunc(inactive, func(a, b *Item) int { return cmp.Compare(a.ended, b.ended) })
	for _, item := range inactive[:len(inactive)-maxInactive] {
		delete(r.items, item.meta.ID)
	}
}

// startWatch and stopWatch run under Registry.mu. The watcher lives only while
// the item is active (including unverified, whose agent still writes the
// transcript); ended and superseded items keep their last stream state.
func (item *Item) startWatch(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	item.cancel, item.done = cancel, done
	path := item.path
	go func() {
		defer close(done)
		item.watch(ctx, path)
	}()
}
func (item *Item) stopWatch() {
	if item.cancel != nil {
		item.cancel()
		item.cancel, item.done = nil, nil
	}
}

// release drops the parsed history of an item that stopped watching. The
// stream keeps its last snapshot for clients; a revive re-reads from the start.
func (item *Item) release() {
	item.mu.Lock()
	defer item.mu.Unlock()
	item.projection = item.adapter().newProjection()
	item.last = nil
}
func (item *Item) watch(ctx context.Context, path string) {
	// A cancelled watcher may still be finishing a read while its successor
	// starts. Checking ctx under item.mu keeps its late results out of the
	// successor's projection: cancel happens before the successor starts.
	apply := func(batch transcript.Batch) error {
		item.mu.Lock()
		defer item.mu.Unlock()
		if ctx.Err() != nil {
			return nil
		}
		return item.applyLocked(batch)
	}
	transcript.Watch(ctx, path, apply, func(err error) {
		item.mu.Lock()
		defer item.mu.Unlock()
		if ctx.Err() != nil {
			return
		}
		if item.lastError != err.Error() {
			item.invalid = true
			item.lastError = err.Error()
			item.stream.Append("session.error", map[string]string{"message": "Transcript unavailable. Open Terminal."})
			slog.Warn("transcript watcher", "error", err)
		}
	})
}
func (item *Item) apply(batch transcript.Batch) error {
	item.mu.Lock()
	defer item.mu.Unlock()
	return item.applyLocked(batch)
}
func (item *Item) applyLocked(batch transcript.Batch) error {
	format := item.adapter()
	if batch.Reset {
		item.projection = format.newProjection()
		item.last = nil
		item.loading = true
		item.invalid = false
	}
	if item.invalid {
		return errors.New("transcript identity requires resync")
	}
	for _, line := range batch.Lines {
		record, err := format.decode(line, item.nativeID)
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
		current = append(current, Message{ID: node.ID, Role: node.Role, Text: node.Text, Timestamp: node.Timestamp, Tool: ToolActivity(node.Tool)})
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
	changes, ok := messageChanges(item.last, current, format.source)
	if !ok {
		item.last = current
		return item.stream.Reset(current)
	}
	if len(changes) == 0 {
		return nil
	}
	item.last = current
	return item.stream.Commit(current, changes)
}

// messageChanges publishes appended messages, and a tool item whose state or
// result changed in place as message.updated with the whole item. Any other
// difference (a branch switch, a changed text message) needs a new snapshot.
func messageChanges(last, current []Message, source string) ([]stream.Change, bool) {
	if len(current) < len(last) {
		return nil, false
	}
	var changes []stream.Change
	for i, prior := range last {
		next := current[i]
		if next == prior {
			continue
		}
		if next.ID != prior.ID || next.Role != "tool" || prior.Role != "tool" {
			return nil, false
		}
		changes = append(changes, stream.Change{Type: "message.updated", Payload: next, Source: source})
	}
	for _, m := range current[len(last):] {
		changes = append(changes, stream.Change{Type: "message." + m.Role, Payload: m, Source: source})
	}
	return changes, true
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
	// item.last survives invalidation, so the preview follows meta.Chat. Tool
	// items have no text to preview.
	for i := len(item.last) - 1; meta.Chat && i >= 0; i-- {
		if last := item.last[i]; last.Role != "tool" {
			meta.LastActivity = last.Timestamp
			meta.LastMessage = &MessagePreview{Role: last.Role, Text: previewText(last.Text)}
			break
		}
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

// Project is the directory Herdr reported for the item. Any item still in
// the registry has one, including ended ones; reading it writes nothing.
func (r *Registry) Project(id string) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	item, ok := r.items[id]
	if !ok {
		return "", errors.New("session not found")
	}
	return item.meta.Project, nil
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
	// The size is a render hint only; an unknown size must not hide the frame.
	frame.Cols, frame.Rows = 0, 0
	if size, err := r.gateway.PaneSize(ctx, meta.PaneID); err == nil {
		frame.Cols, frame.Rows = size.Cols, size.Rows
	}
	current, err = r.gateway.Binding(ctx, meta.PaneID)
	if err != nil || current.Token != binding {
		return herdr.TerminalSnapshot{}, errors.New("session binding changed")
	}
	return frame, nil
}
