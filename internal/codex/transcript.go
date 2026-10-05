// Package codex reads Codex CLI rollout JSONL without writing conversation data.
package codex

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/psw7205/herdr-remote/internal/transcript"
	"strconv"
	"strings"
)

var ErrSessionMismatch = errors.New("rollout session identity mismatch")

type Record = transcript.Record

// userTextKind marks text the user typed. Codex also writes injected context
// (AGENTS.md, environment, internal goals) as role "user" items, told apart
// only by internal_chat_message_metadata_passthrough.content_item_kinds.
const userTextKind = "user.text"

// Decode returns a Record only for response_item messages of role user or
// assistant. Its ID is the record's own ordinal, which Codex writes into every
// rollout record, so it does not depend on where the reader started. event_msg
// records repeat the same messages and are ignored, as are unknown types.
func Decode(line []byte, sessionID string) (Record, error) {
	var native struct {
		Type      string          `json:"type"`
		Timestamp string          `json:"timestamp"`
		Ordinal   json.RawMessage `json:"ordinal"`
		Payload   json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(line, &native); err != nil {
		return Record{}, fmt.Errorf("decode Codex record: %w", err)
	}
	switch native.Type {
	case "session_meta":
		var meta struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(native.Payload, &meta); err != nil {
			return Record{}, fmt.Errorf("decode Codex session_meta: %w", err)
		}
		if sessionID == "" || meta.ID != sessionID {
			return Record{}, ErrSessionMismatch
		}
		return Record{}, nil
	case "response_item":
	default:
		return Record{}, nil
	}
	var item struct {
		Type        string          `json:"type"`
		Role        string          `json:"role"`
		Content     json.RawMessage `json:"content"`
		Passthrough *struct {
			Kinds []string `json:"content_item_kinds"`
		} `json:"internal_chat_message_metadata_passthrough"`
	}
	if err := json.Unmarshal(native.Payload, &item); err != nil {
		return Record{}, fmt.Errorf("decode Codex response_item: %w", err)
	}
	if item.Type != "message" || item.Role != "user" && item.Role != "assistant" {
		return Record{}, nil
	}
	var ordinal uint64
	if len(native.Ordinal) == 0 || string(native.Ordinal) == "null" || json.Unmarshal(native.Ordinal, &ordinal) != nil {
		return Record{}, errors.New("invalid Codex message identity")
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(item.Content, &blocks); err != nil {
		return Record{}, fmt.Errorf("unsupported Codex message content: %w", err)
	}
	// A user item counts as typed only through its own aligned kind. Without
	// the passthrough (older rollouts) or with misaligned kinds, the record
	// stays hidden: showing injected context as the user's words is worse than
	// a gap that the PC's Herdr pane still shows.
	var kinds []string
	if item.Passthrough != nil && len(item.Passthrough.Kinds) == len(blocks) {
		kinds = item.Passthrough.Kinds
	}
	var parts []string
	for i, b := range blocks {
		switch {
		case item.Role == "assistant" && b.Type == "output_text":
			parts = append(parts, b.Text)
		case item.Role == "user" && kinds != nil && kinds[i] == userTextKind && b.Type == "input_text":
			parts = append(parts, b.Text)
		}
	}
	return Record{ID: strconv.FormatUint(ordinal, 10), Role: item.Role, Text: strings.Join(parts, "\n"), Timestamp: native.Timestamp}, nil
}
