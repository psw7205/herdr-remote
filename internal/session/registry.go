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
}

func NewRegistry(ctx context.Context, gateway Gateway, claudeRoot string) *Registry {
	return &Registry{ctx: ctx, gateway: gateway, claudeRoot: claudeRoot, items: make(map[string]*Item)}
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
func (r *Registry) Refresh(ctx context.Context) error {
	snapshot, err := r.gateway.Snapshot(ctx)
	if err != nil {
		return err
	}
	found := make(map[string]Meta)
	paths := make(map[string]string)
	for _, a := range snapshot.Agents {
		if a.Agent != "claude" || a.PaneID == "" {
			continue
		}
		meta := Meta{ID: "pane:" + a.PaneID, Agent: a.Agent, PaneID: a.PaneID, Project: a.CWD, Title: a.Title, Status: status(a.Status), Active: true}
		binding, e := r.gateway.Binding(ctx, a.PaneID)
		if e == nil && binding.Agent == "claude" && binding.TerminalID == a.TerminalID && binding.Token != "" && binding.ProcessID != 0 {
			meta.Binding = binding.Token
			meta.Project = binding.CWD
			meta.Terminal = true
			if path, e := claude.Resolve(r.claudeRoot, binding.NativeSessionID); e == nil {
				meta.ID = "claude:" + binding.NativeSessionID
				meta.Chat = true
				paths[meta.ID] = path
			}
		}
		found[meta.ID] = meta
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, item := range r.items {
		if _, ok := found[id]; !ok && item.meta.Active {
			item.meta.Active = false
			item.meta.Binding = ""
			item.meta.Terminal = false
			item.meta.Status = "completed"
			item.stream.Append("agent.status", map[string]any{"status": "completed", "lifecycle": "ended"})
		}
	}
	for id, meta := range found {
		if item, ok := r.items[id]; ok {
			if item.meta.Binding != meta.Binding || item.meta.Status != meta.Status || item.meta.Active != meta.Active {
				item.stream.Append("agent.status", map[string]any{"status": meta.Status, "lifecycle": "active"})
			}
			item.meta = meta
			continue
		}
		item := &Item{meta: meta, path: paths[id], stream: stream.New(id), projection: claude.NewProjection()}
		if meta.Chat {
			item.nativeID = id[len("claude:"):]
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
