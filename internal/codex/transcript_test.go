package codex

import (
	"encoding/json"
	"errors"
	"testing"
)

const session = "01a0aaaa-0000-7000-8000-000000000001"

func line(t *testing.T, ordinal any, typ string, payload map[string]any) []byte {
	t.Helper()
	record := map[string]any{"timestamp": "2026-10-01T00:00:00.000Z", "type": typ, "payload": payload}
	if ordinal != nil {
		record["ordinal"] = ordinal
	}
	out, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func message(role string, items []map[string]any, kinds []string) map[string]any {
	payload := map[string]any{"type": "message", "id": "msg_sample", "role": role, "content": items}
	if kinds != nil {
		payload["internal_chat_message_metadata_passthrough"] = map[string]any{"content_item_kinds": kinds, "turn_id": "turn-sample"}
	}
	return payload
}

func text(typ, value string) map[string]any { return map[string]any{"type": typ, "text": value} }

func TestDecodeShowsOnlyTypedUserText(t *testing.T) {
	r, err := Decode(line(t, 7, "response_item", message("user", []map[string]any{text("input_text", "typed prompt")}, []string{"user.text"})), session)
	if err != nil {
		t.Fatal(err)
	}
	if r.ID != "7" || r.Role != "user" || r.Text != "typed prompt" || r.Timestamp != "2026-10-01T00:00:00.000Z" || r.ParentID != "" {
		t.Fatalf("wrong record: %+v", r)
	}
}

func TestDecodeHidesInjectedUserItems(t *testing.T) {
	for name, payload := range map[string]map[string]any{
		"startup context":  message("user", []map[string]any{text("input_text", "agents file"), text("input_text", "environment")}, []string{"agents_md.instructions", "environments.environment_context"}),
		"internal context": message("user", []map[string]any{text("input_text", "goal")}, []string{"goal.internal_context"}),
		"developer":        message("developer", []map[string]any{text("input_text", "instructions")}, []string{"permissions.instructions"}),
		"developer typed":  message("developer", []map[string]any{text("input_text", "instructions")}, []string{"user.text"}),
		// Rollouts written before the passthrough existed cannot tell a prompt
		// from injected context, so their user records stay hidden.
		"missing kinds":    message("user", []map[string]any{text("input_text", "unknown origin")}, nil),
		"kinds misaligned": message("user", []map[string]any{text("input_text", "a"), text("input_text", "b")}, []string{"user.text"}),
		"non-text item":    message("user", []map[string]any{{"type": "input_image", "image_url": "data:"}}, []string{"user.text"}),
	} {
		r, err := Decode(line(t, 3, "response_item", payload), session)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if r.Text != "" {
			t.Fatalf("%s exposed: %+v", name, r)
		}
	}
}

func TestDecodeKeepsOnlyTypedItemsOfMixedUserRecord(t *testing.T) {
	payload := message("user", []map[string]any{text("input_text", "injected"), text("input_text", "typed")}, []string{"environments.environment_context", "user.text"})
	r, err := Decode(line(t, 4, "response_item", payload), session)
	if err != nil {
		t.Fatal(err)
	}
	if r.Text != "typed" {
		t.Fatalf("mixed record: %+v", r)
	}
}

func TestDecodeAssistantOutputTextRegardlessOfKinds(t *testing.T) {
	payload := message("assistant", []map[string]any{text("output_text", "first"), text("output_text", "second")}, []string{"unknown", "unknown"})
	payload["phase"] = "final_answer"
	r, err := Decode(line(t, 9, "response_item", payload), session)
	if err != nil {
		t.Fatal(err)
	}
	if r.Role != "assistant" || r.Text != "first\nsecond" || r.ID != "9" {
		t.Fatalf("assistant record: %+v", r)
	}
}

func TestDecodeIgnoresNonMessageRecords(t *testing.T) {
	for _, l := range [][]byte{
		line(t, 1, "response_item", map[string]any{"type": "reasoning", "summary": []any{}}),
		line(t, 2, "response_item", map[string]any{"type": "function_call", "name": "shell", "arguments": "{}"}),
		line(t, 3, "response_item", map[string]any{"type": "function_call_output", "output": "out"}),
		line(t, 4, "response_item", map[string]any{"type": "custom_tool_call", "input": "x"}),
		line(t, 5, "response_item", map[string]any{"type": "custom_tool_call_output", "output": "out"}),
		line(t, 6, "event_msg", map[string]any{"type": "item_completed", "item": map[string]any{"type": "agent_message", "text": "duplicate"}}),
		line(t, 7, "event_msg", map[string]any{"type": "task_complete", "last_agent_message": "duplicate"}),
		line(t, 8, "event_msg", map[string]any{"type": "user_message", "message": "duplicate"}),
		line(t, 9, "turn_context", map[string]any{"cwd": "/work"}),
		line(t, 10, "world_state", map[string]any{}),
		line(t, 11, "token_usage_record", map[string]any{}),
		line(t, 12, "future_type", map[string]any{"text": "unknown"}),
		line(t, nil, "event_msg", map[string]any{"type": "token_count"}),
	} {
		r, err := Decode(l, session)
		if err != nil {
			t.Fatalf("%s: %v", l, err)
		}
		if r.ID != "" || r.Text != "" {
			t.Fatalf("non-message record became Chat: %+v", r)
		}
	}
}

func TestDecodeVerifiesSessionMeta(t *testing.T) {
	if _, err := Decode(line(t, 0, "session_meta", map[string]any{"id": session, "source": "cli"}), session); err != nil {
		t.Fatal(err)
	}
	other := line(t, 0, "session_meta", map[string]any{"id": "01a0aaaa-0000-7000-8000-000000000002"})
	if _, err := Decode(other, session); !errors.Is(err, ErrSessionMismatch) {
		t.Fatalf("foreign session_meta: %v", err)
	}
	if _, err := Decode(line(t, 0, "session_meta", map[string]any{"id": session}), ""); !errors.Is(err, ErrSessionMismatch) {
		t.Fatalf("empty expected session: %v", err)
	}
}

func TestDecodeRejectsMalformedAndUnidentifiedMessages(t *testing.T) {
	for name, l := range map[string][]byte{
		"malformed":          []byte(`{"type":`),
		"missing ordinal":    line(t, nil, "response_item", message("assistant", []map[string]any{text("output_text", "a")}, nil)),
		"non-integer":        line(t, "7", "response_item", message("assistant", []map[string]any{text("output_text", "a")}, nil)),
		"negative ordinal":   line(t, -1, "response_item", message("assistant", []map[string]any{text("output_text", "a")}, nil)),
		"content not a list": line(t, 1, "response_item", map[string]any{"type": "message", "role": "assistant", "content": "plain"}),
	} {
		if _, err := Decode(l, session); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
}
