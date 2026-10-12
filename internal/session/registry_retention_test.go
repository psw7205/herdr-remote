package session

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/psw7205/herdr-remote/internal/herdr"
	"github.com/psw7205/herdr-remote/internal/stream"
	"runtime"
	"strings"
	"testing"
	"weak"
)

// An ended item keeps its stream ring and state for viewers until
// evictInactive drops it; after that the registry holds nothing that keeps
// the stream reachable.
func TestEndedItemKeepsStreamUntilEvicted(t *testing.T) {
	r, f, _ := boundRegistry(t)
	id := "claude:" + nativeA
	done := watcherOf(r, id)
	hub, err := r.Stream(id)
	if err != nil {
		t.Fatal(err)
	}
	epoch := hub.Snapshot().Cursor.Epoch
	payload := json.RawMessage(`"` + strings.Repeat("x", 8190) + `"`)
	const events = 64
	for range events {
		hub.Append("message.updated", payload)
	}
	f.agents = nil
	if err := r.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitClosed(t, done)
	replay, _, stop := hub.Subscribe(stream.Cursor{Epoch: epoch})
	stop()
	kept := 0
	for _, e := range replay {
		if e.Type == "message.updated" {
			kept++
		}
	}
	if kept != events || replay[len(replay)-1].Type != "agent.status" {
		t.Fatalf("ended item replays %d of %d events, last %q", kept, events, replay[len(replay)-1].Type)
	}

	r.mu.RLock()
	item := weak.Make(r.items[id])
	r.mu.RUnlock()
	ring := weak.Make(hub)
	hub = nil
	refreshPane := func(i int) {
		t.Helper()
		f.agents = []herdr.Agent{{PaneID: fmt.Sprintf("w2:p%d", i), TerminalID: "t", Agent: "claude", Status: "idle"}}
		if err := r.Refresh(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	for i := range maxInactive {
		refreshPane(i)
	}
	runtime.GC()
	if _, err := r.Stream(id); err != nil || ring.Value() == nil {
		t.Fatalf("ended item released before eviction: %v", err)
	}
	refreshPane(maxInactive)
	if _, err := r.Stream(id); err == nil {
		t.Fatal("oldest ended item not evicted")
	}
	for range 10 {
		runtime.GC()
		if item.Value() == nil && ring.Value() == nil {
			return
		}
	}
	t.Fatal("evicted item and its stream are still reachable")
}
