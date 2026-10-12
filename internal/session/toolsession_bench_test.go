package session

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/psw7205/herdr-remote/internal/claude"
	"github.com/psw7205/herdr-remote/internal/stream"
	"github.com/psw7205/herdr-remote/internal/transcript"
)

type toolSessionCase struct{ tools, result int }

var toolSessionGrid = []toolSessionCase{
	{100, 200}, {1000, 200}, {5000, 200},
	{100, 8192}, {1000, 8192}, {5000, 8192},
}

// syntheticTool is the generator in internal/claude's benchmarks; keep the
// two identical so both packages measure the same shape.
func syntheticTool(session string, i int, result string) [][]byte {
	var lines [][]byte
	parent := ""
	if i > 0 {
		parent = syntheticID(i-1, 3)
	}
	timestamp := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Second).Format("2006-01-02T15:04:05.000Z")
	add := func(kind int, typ string, message, extra map[string]any) {
		record := map[string]any{"parentUuid": nil, "isSidechain": false, "userType": "external", "cwd": "/work/repo", "sessionId": session, "version": "2.0.0", "gitBranch": "main", "type": typ, "uuid": syntheticID(i, kind), "timestamp": timestamp, "message": message}
		if parent != "" {
			record["parentUuid"] = parent
		}
		maps.Copy(record, extra)
		var b bytes.Buffer
		encoder := json.NewEncoder(&b)
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(record); err != nil {
			panic(err)
		}
		lines = append(lines, bytes.TrimSuffix(b.Bytes(), []byte("\n")))
		parent = syntheticID(i, kind)
	}
	turn := fmt.Sprintf("msg_synthetic_%06d", i)
	assistant := func(block map[string]any) map[string]any {
		usage := map[string]any{"input_tokens": 4, "cache_creation_input_tokens": 512, "cache_read_input_tokens": 40960, "output_tokens": 96, "service_tier": "standard"}
		return map[string]any{"id": turn, "type": "message", "role": "assistant", "model": "claude-synthetic", "content": []any{block}, "stop_reason": "tool_use", "stop_sequence": nil, "usage": usage}
	}
	request := map[string]any{"requestId": fmt.Sprintf("req_synthetic_%06d", i)}
	if i%50 == 0 {
		add(0, "user", map[string]any{"role": "user", "content": fmt.Sprintf("Synthetic prompt %d: run the package tests and fix any failure.", i)}, nil)
	}
	if i%4 == 0 {
		add(1, "assistant", assistant(map[string]any{"type": "text", "text": strings.Repeat("Synthetic progress note before the next tool call. ", 4)}), request)
	}
	tool := fmt.Sprintf("toolu_synthetic_%06d", i)
	input := map[string]any{"command": fmt.Sprintf("go test ./internal/pkg%04d/...", i), "description": "Run package tests"}
	add(2, "assistant", assistant(map[string]any{"type": "tool_use", "id": tool, "name": "Bash", "input": input}), request)
	content := []any{map[string]any{"type": "tool_result", "tool_use_id": tool, "content": result, "is_error": false}}
	add(3, "user", map[string]any{"role": "user", "content": content}, map[string]any{"toolUseResult": map[string]any{"stdout": result, "stderr": "", "interrupted": false, "isImage": false}})
	return lines
}

func syntheticID(i, kind int) string { return fmt.Sprintf("%08x-0000-4000-8000-%012x", i, kind) }

func syntheticResult(size int) string {
	const line = "ok  \texample.com/repo/internal/pkg\t0.012s -> synthetic output\n"
	return strings.Repeat(line, size/len(line)+1)[:size]
}

func syntheticFile(tb testing.TB, tools, resultBytes int) (string, int64) {
	tb.Helper()
	path := filepath.Join(tb.TempDir(), "session.jsonl")
	f, err := os.Create(path)
	if err != nil {
		tb.Fatal(err)
	}
	result := syntheticResult(resultBytes)
	var size int64
	for i := range tools {
		for _, line := range syntheticTool(nativeA, i, result) {
			n, err := f.Write(append(line, '\n'))
			if err != nil {
				tb.Fatal(err)
			}
			size += int64(n)
		}
	}
	if err := f.Close(); err != nil {
		tb.Fatal(err)
	}
	return path, size
}

func syntheticItem() *Item {
	return &Item{nativeID: nativeA, meta: Meta{ID: "claude:" + nativeA, Chat: true, Active: true}, projection: claude.NewProjection(), stream: stream.New("claude:" + nativeA)}
}

// loadSynthetic reads the transcript the way the watcher's first pass does:
// Tailer batches of at most 8 MiB, and one stream Reset at the end.
func loadSynthetic(tb testing.TB, path string) *Item {
	tb.Helper()
	item := syntheticItem()
	tailer := transcript.NewTailer(path)
	for {
		batch, err := tailer.Read()
		if err != nil {
			tb.Fatal(err)
		}
		if err := item.apply(batch); err != nil {
			tb.Fatal(err)
		}
		if !batch.More {
			return item
		}
	}
}

func toolSessionName(tools, result int) string {
	return fmt.Sprintf("tools=%d/result=%dB", tools, result)
}

func mustMarshal(tb testing.TB, v any) []byte {
	tb.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		tb.Fatal(err)
	}
	return b
}

// liveChanges are the changes a live session publishes when every native
// record arrives in a batch of its own: each call is appended running and
// then updated with its result.
func liveChanges(messages []Message) ([]stream.Change, []int) {
	var changes []stream.Change
	var tools []int
	n := 0
	for _, m := range messages {
		if m.Role != "tool" {
			changes = append(changes, stream.Change{Type: "message." + m.Role, Payload: m, Source: claudeAdapter.source})
			tools = append(tools, n+1)
			continue
		}
		running := m
		running.Tool.State, running.Tool.Result, running.Tool.ResultTruncated = transcript.ToolRunning, "", false
		n++
		changes = append(changes, stream.Change{Type: "message.tool", Payload: running, Source: claudeAdapter.source}, stream.Change{Type: "message.updated", Payload: m, Source: claudeAdapter.source})
		tools = append(tools, n, n)
	}
	return changes, tools
}

type ringResult struct {
	events, bytes int
	// oldest is the first event still in the ring, 0-based.
	oldest int
	// countTrim and byteTrim are the 1-based tool calls whose events first
	// made the ring drop an event because of its event or byte limit; 0 if
	// that limit never did. A text event counts as the call after it.
	countTrim, byteTrim int
}

// replayRing commits changes one at a time and tracks the ring the stream is
// expected to keep, then checks that against what Subscribe replays, so a
// change of the stream's limits fails here instead of skewing the numbers.
func replayRing(tb testing.TB, changes []stream.Change, tools []int) ringResult {
	tb.Helper()
	sizes := make([]int, len(changes))
	for i, c := range changes {
		sizes[i] = len(mustMarshal(tb, c.Payload))
	}
	s := stream.New("ring")
	var r ringResult
	for i, c := range changes {
		if err := s.Commit(nil, []stream.Change{c}); err != nil {
			tb.Fatal(err)
		}
		r.events++
		r.bytes += sizes[i]
		for r.events > 1024 || r.bytes > 8<<20 {
			if r.events > 1024 && r.countTrim == 0 {
				r.countTrim = tools[i]
			}
			if r.events <= 1024 && r.byteTrim == 0 {
				r.byteTrim = tools[i]
			}
			r.bytes -= sizes[r.oldest]
			r.oldest++
			r.events--
		}
	}
	epoch := s.Snapshot().Cursor.Epoch
	replay, _, stop := s.Subscribe(stream.Cursor{Epoch: epoch, Sequence: uint64(r.oldest)})
	stop()
	got := 0
	for _, e := range replay {
		got += len(e.Payload)
	}
	if len(replay) != r.events || got != r.bytes || len(replay) > 0 && replay[0].Type == "session.snapshot" {
		tb.Fatalf("ring holds %d events, %d bytes; expected %d, %d", len(replay), got, r.events, r.bytes)
	}
	if r.oldest > 0 {
		older, _, stop := s.Subscribe(stream.Cursor{Epoch: epoch, Sequence: uint64(r.oldest - 1)})
		stop()
		if len(older) != 1 || older[0].Type != "session.snapshot" {
			tb.Fatalf("ring still replays from sequence %d", r.oldest)
		}
	}
	return r
}

func TestSyntheticLiveSessionPublishesRunningThenCompleted(t *testing.T) {
	item := syntheticItem()
	if err := item.apply(transcript.Batch{Reset: true}); err != nil {
		t.Fatal(err)
	}
	before := item.stream.Snapshot().Cursor
	_, live, stop := item.stream.Subscribe(before)
	defer stop()
	var got []stream.Event
	result := syntheticResult(8193)
	for i := range 60 {
		for _, line := range syntheticTool(nativeA, i, result) {
			if err := item.apply(transcript.Batch{Lines: [][]byte{line}}); err != nil {
				t.Fatal(err)
			}
			for len(live) > 0 {
				got = append(got, <-live)
			}
		}
	}
	if item.stream.Snapshot().Cursor.Epoch != before.Epoch {
		t.Fatal("a tool result reset the stream")
	}
	want, _ := liveChanges(item.last)
	if len(got) != len(want) || len(want) != 2*60+15+2 {
		t.Fatalf("%d live events, %d derived", len(got), len(want))
	}
	for i, e := range got {
		if e.Type != want[i].Type || !bytes.Equal(e.Payload, mustMarshal(t, want[i].Payload)) {
			t.Fatalf("event %d: %s %s, derived %s", i, e.Type, e.Payload, want[i].Type)
		}
	}
}

func TestReplayRingFollowsBothStreamLimits(t *testing.T) {
	small := Message{ID: "a/t", Role: "tool", Tool: ToolActivity{ID: "t", Name: "Bash", State: transcript.ToolCompleted}}
	large := small
	large.Tool.Result = strings.Repeat("x", 64<<10)
	for _, c := range []struct {
		name    string
		message Message
		calls   int
		count   bool
	}{{"events", small, 600, true}, {"bytes", large, 200, false}} {
		messages := make([]Message, c.calls)
		for i := range messages {
			messages[i] = c.message
		}
		changes, tools := liveChanges(messages)
		r := replayRing(t, changes, tools)
		if c.count != (r.countTrim > 0) || c.count == (r.byteTrim > 0) {
			t.Fatalf("%s: wrong limit: %+v", c.name, r)
		}
	}
}

func BenchmarkToolSessionLoad(b *testing.B) {
	grid := append(slices.Clone(toolSessionGrid), toolSessionCase{100, 32768}, toolSessionCase{1000, 32768})
	for _, c := range grid {
		b.Run(toolSessionName(c.tools, c.result), func(b *testing.B) {
			path, size := syntheticFile(b, c.tools, c.result)
			b.SetBytes(size)
			for b.Loop() {
				loadSynthetic(b, path)
			}
			var before, after runtime.MemStats
			runtime.GC()
			runtime.ReadMemStats(&before)
			item := loadSynthetic(b, path)
			runtime.GC()
			runtime.ReadMemStats(&after)
			heap := float64(after.HeapAlloc) - float64(before.HeapAlloc)
			state := len(item.stream.Snapshot().Data)
			toolBytes, tools := 0, 0
			for _, m := range item.last {
				if m.Role == "tool" {
					toolBytes += len(mustMarshal(b, m))
					tools++
				}
			}
			if tools != c.tools {
				b.Fatalf("%d tool items", tools)
			}
			runtime.KeepAlive(item)
			b.ReportMetric(float64(size), "transcript-B")
			b.ReportMetric(float64(state), "state-B")
			b.ReportMetric(float64(toolBytes)/float64(tools), "B/tool-item")
			b.ReportMetric(heap, "heap-B")
			b.ReportMetric(float64(after.HeapInuse)-float64(before.HeapInuse), "heapinuse-B")
			b.ReportMetric(heap/float64(size), "heap/transcript")
			b.ReportMetric(heap/float64(state), "heap/state")
		})
	}
}

// copy is stream.Snapshot alone; http encodes the snapshot response the way
// the handler does; resync is a WS subscribe with a stale cursor plus the
// marshal that sends its session.snapshot event.
func BenchmarkToolSessionSnapshot(b *testing.B) {
	for _, c := range toolSessionGrid {
		b.Run(toolSessionName(c.tools, c.result), func(b *testing.B) {
			path, _ := syntheticFile(b, c.tools, c.result)
			item := loadSynthetic(b, path)
			registry := &Registry{gateway: &fakeHerdr{}, items: map[string]*Item{item.meta.ID: item}}
			state := int64(len(item.stream.Snapshot().Data))
			b.Run("copy", func(b *testing.B) {
				b.SetBytes(state)
				for b.Loop() {
					item.stream.Snapshot()
				}
			})
			b.Run("http", func(b *testing.B) {
				b.SetBytes(state)
				for b.Loop() {
					snapshot, meta, err := registry.Get(item.meta.ID)
					if err != nil {
						b.Fatal(err)
					}
					if err := json.NewEncoder(io.Discard).Encode(map[string]any{"session": meta, "snapshot": snapshot}); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("resync", func(b *testing.B) {
				b.SetBytes(state)
				for b.Loop() {
					replay, _, stop := item.stream.Subscribe(stream.Cursor{})
					stop()
					if _, err := json.Marshal(replay[0]); err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}

// One op is one more tool call arriving live, a record per batch, so every
// commit marshals the whole state. The session grows by one call per op;
// tools-end reports where it ended.
func BenchmarkToolSessionLiveCall(b *testing.B) {
	for _, c := range toolSessionGrid {
		b.Run(toolSessionName(c.tools, c.result), func(b *testing.B) {
			path, _ := syntheticFile(b, c.tools, c.result)
			item := loadSynthetic(b, path)
			result := syntheticResult(c.result)
			start := item.stream.Snapshot().Cursor
			next := c.tools
			for b.Loop() {
				for _, line := range syntheticTool(nativeA, next, result) {
					if err := item.apply(transcript.Batch{Lines: [][]byte{line}}); err != nil {
						b.Fatal(err)
					}
				}
				next++
			}
			end := item.stream.Snapshot().Cursor
			if end.Epoch != start.Epoch {
				b.Fatal("a live call reset the stream")
			}
			b.ReportMetric(float64(end.Sequence-start.Sequence)/float64(next-c.tools), "commits/op")
			b.ReportMetric(float64(next), "tools-end")
		})
	}
}

// One op replays a whole session's live events into a fresh stream without
// the state marshal; the metrics describe the ring it leaves.
func BenchmarkToolSessionEventRing(b *testing.B) {
	for _, c := range toolSessionGrid {
		b.Run(toolSessionName(c.tools, c.result), func(b *testing.B) {
			path, _ := syntheticFile(b, c.tools, c.result)
			changes, tools := liveChanges(loadSynthetic(b, path).last)
			var r ringResult
			for b.Loop() {
				r = replayRing(b, changes, tools)
			}
			var running, updated, n int
			for _, c := range changes {
				switch c.Type {
				case "message.tool":
					running += len(mustMarshal(b, c.Payload))
					n++
				case "message.updated":
					updated += len(mustMarshal(b, c.Payload))
				}
			}
			b.ReportMetric(float64(len(changes)), "events")
			b.ReportMetric(float64(r.events), "ring-events")
			b.ReportMetric(float64(r.bytes), "ring-B")
			b.ReportMetric(float64(tools[len(tools)-1]-tools[r.oldest]+1), "ring-tools")
			b.ReportMetric(float64(r.countTrim), "count-trim-tool")
			b.ReportMetric(float64(r.byteTrim), "byte-trim-tool")
			b.ReportMetric(float64(running)/float64(n), "B/running")
			b.ReportMetric(float64(updated)/float64(n), "B/updated")
		})
	}
}
