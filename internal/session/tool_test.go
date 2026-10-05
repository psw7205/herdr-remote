package session

import (
	"encoding/json"
	"testing"

	"github.com/psw7205/herdr-remote/internal/claude"
	"github.com/psw7205/herdr-remote/internal/stream"
	"github.com/psw7205/herdr-remote/internal/transcript"
)

func toolLines(native string) map[string][]byte {
	record := func(s string) []byte {
		return []byte(`{"sessionId":"` + native + `","timestamp":"2026-10-05T00:00:00Z","cwd":"/work/repo",` + s + `}`)
	}
	return map[string][]byte{
		"prompt": record(`"type":"user","uuid":"u1","parentUuid":null,"message":{"role":"user","content":"run the tests"}`),
		"use":    record(`"type":"assistant","uuid":"a1","parentUuid":"u1","message":{"role":"assistant","id":"m1","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"go test ./..."}}]}`),
		"result": record(`"type":"user","uuid":"r1","parentUuid":"a1","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}`),
		"answer": record(`"type":"assistant","uuid":"b1","parentUuid":"r1","message":{"role":"assistant","id":"m2","content":[{"type":"text","text":"All tests pass."}]}`),
	}
}

func decodeMessages(t *testing.T, raw json.RawMessage) []Message {
	t.Helper()
	var out []Message
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestToolResultUpdatesTheItemWithoutANewEpoch(t *testing.T) {
	lines := toolLines(nativeA)
	item := &Item{nativeID: nativeA, meta: Meta{ID: "claude:" + nativeA, Chat: true, Active: true}, projection: claude.NewProjection(), stream: stream.New("claude:" + nativeA)}
	if err := item.apply(transcript.Batch{Reset: true, Lines: [][]byte{lines["prompt"], lines["use"]}}); err != nil {
		t.Fatal(err)
	}
	before := item.stream.Snapshot()
	running := decodeMessages(t, before.Data)
	if len(running) != 2 || running[1].Role != "tool" || running[1].Tool.State != transcript.ToolRunning || running[1].Tool.Summary != "go test ./..." {
		t.Fatalf("wrong running snapshot: %+v", running)
	}
	_, live, stop := item.stream.Subscribe(before.Cursor)
	defer stop()

	if err := item.apply(transcript.Batch{Lines: [][]byte{lines["result"], lines["answer"]}}); err != nil {
		t.Fatal(err)
	}
	after := item.stream.Snapshot()
	if after.Cursor.Epoch != before.Cursor.Epoch || after.Cursor.Sequence != before.Cursor.Sequence+2 {
		t.Fatalf("result reset the stream: %+v -> %+v", before.Cursor, after.Cursor)
	}
	final := decodeMessages(t, after.Data)
	if len(final) != 3 || final[1].Tool.State != transcript.ToolCompleted || final[1].Tool.Result != "ok" || final[2].Text != "All tests pass." {
		t.Fatalf("snapshot lacks the final state: %+v", final)
	}
	var types []string
	for range 2 {
		event := <-live
		types = append(types, event.Type)
		if event.Type == "message.updated" {
			var updated Message
			if err := json.Unmarshal(event.Payload, &updated); err != nil || updated != final[1] {
				t.Fatalf("update does not carry the whole item: %s", event.Payload)
			}
		}
	}
	if types[0] != "message.updated" || types[1] != "message.assistant" {
		t.Fatalf("wrong live order: %v", types)
	}

	// A client that missed both events replays them from its cursor.
	replay, _, stopReplay := item.stream.Subscribe(before.Cursor)
	defer stopReplay()
	if len(replay) != 2 || replay[0].Type != "message.updated" || replay[1].Type != "message.assistant" {
		t.Fatalf("wrong replay: %+v", replay)
	}
}

func TestNewToolItemIsAppendedAsMessageTool(t *testing.T) {
	lines := toolLines(nativeA)
	item := &Item{nativeID: nativeA, projection: claude.NewProjection(), stream: stream.New("claude:" + nativeA)}
	if err := item.apply(transcript.Batch{Reset: true, Lines: [][]byte{lines["prompt"]}}); err != nil {
		t.Fatal(err)
	}
	before := item.stream.Snapshot().Cursor
	if err := item.apply(transcript.Batch{Lines: [][]byte{lines["use"]}}); err != nil {
		t.Fatal(err)
	}
	replay, _, stop := item.stream.Subscribe(before)
	defer stop()
	if len(replay) != 1 || replay[0].Type != "message.tool" || item.stream.Snapshot().Cursor.Epoch != before.Epoch {
		t.Fatalf("wrong append: %+v", replay)
	}
	var tool map[string]any
	if err := json.Unmarshal(replay[0].Payload, &tool); err != nil || tool["role"] != "tool" || tool["tool"].(map[string]any)["state"] != transcript.ToolRunning {
		t.Fatalf("wrong payload: %s", replay[0].Payload)
	}
}

func TestTextMessagesCarryNoToolField(t *testing.T) {
	b, err := json.Marshal(Message{ID: "u1", Role: "user", Text: "hi", Timestamp: "t"})
	if err != nil || string(b) != `{"id":"u1","role":"user","text":"hi","timestamp":"t"}` {
		t.Fatalf("text message wire shape changed: %s %v", b, err)
	}
}

func TestMessageChangesNeedASnapshotForAnythingButToolUpdates(t *testing.T) {
	text := Message{ID: "a1", Role: "assistant", Text: "one"}
	tool := Message{ID: "a2/t1", Role: "tool", Tool: ToolActivity{ID: "t1", Name: "Bash", State: "running"}}
	done := tool
	done.Tool.State = "completed"
	for _, c := range []struct {
		name          string
		last, current []Message
		ok            bool
		types         []string
	}{
		{"unchanged", []Message{text, tool}, []Message{text, tool}, true, nil},
		{"tool update and append", []Message{text, tool}, []Message{text, done, text}, true, []string{"message.updated", "message.assistant"}},
		{"text changed", []Message{text}, []Message{{ID: "a1", Role: "assistant", Text: "two"}}, false, nil},
		{"tool replaced by another", []Message{text, tool}, []Message{text, {ID: "a3/t2", Role: "tool"}}, false, nil},
		{"shorter", []Message{text, tool}, []Message{text}, false, nil},
	} {
		changes, ok := messageChanges(c.last, c.current, "claude.transcript")
		var types []string
		for _, change := range changes {
			types = append(types, change.Type)
		}
		if ok != c.ok || len(types) != len(c.types) || ok && len(types) > 0 && types[0] != c.types[0] {
			t.Errorf("%s: got %v %v", c.name, ok, types)
		}
	}
}

func TestPreviewSkipsTrailingToolItems(t *testing.T) {
	lines := toolLines(nativeA)
	item := &Item{nativeID: nativeA, meta: Meta{ID: "claude:" + nativeA, PaneID: "w1:p2", Chat: true, Active: true, Status: "working"}, projection: claude.NewProjection(), stream: stream.New("claude:" + nativeA)}
	if err := item.apply(transcript.Batch{Reset: true, Lines: [][]byte{lines["prompt"], lines["use"]}}); err != nil {
		t.Fatal(err)
	}
	registry := &Registry{gateway: &fakeHerdr{}, items: map[string]*Item{item.meta.ID: item}}
	if _, meta, _ := registry.Get(item.meta.ID); meta.LastMessage == nil || *meta.LastMessage != (MessagePreview{Role: "user", Text: "run the tests"}) {
		t.Fatalf("tool item became the preview: %+v", meta.LastMessage)
	}
}
