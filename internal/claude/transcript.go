// Package claude reads native JSONL records without writing conversation data.
package claude

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var ErrSessionMismatch = errors.New("transcript session identity mismatch")

// Record retains non-message graph nodes so filtering tool output does not
// break parent traversal. Only Text is eligible for Chat rendering.
type Record struct {
	ID        string
	ParentID  string
	Role      string
	Text      string
	Timestamp string
}

func Decode(line []byte, sessionID string) (Record, error) {
	var native struct {
		Type      string `json:"type"`
		ID        string `json:"uuid"`
		ParentID  string `json:"parentUuid"`
		SessionID string `json:"sessionId"`
		Sidechain bool   `json:"isSidechain"`
		Timestamp string `json:"timestamp"`
		Message   struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(line, &native); err != nil {
		return Record{}, fmt.Errorf("decode Claude record: %w", err)
	}
	if native.SessionID != "" && native.SessionID != sessionID {
		return Record{}, ErrSessionMismatch
	}
	if native.Sidechain {
		return Record{}, nil
	}
	r := Record{ID: native.ID, ParentID: native.ParentID, Timestamp: native.Timestamp}
	if native.Type != "user" && native.Type != "assistant" {
		return r, nil
	}
	if sessionID == "" || native.SessionID != sessionID {
		return Record{}, ErrSessionMismatch
	}
	if native.ID == "" || native.Message.Role != native.Type {
		return Record{}, errors.New("invalid Claude message identity")
	}
	r.Role = native.Type
	var text string
	if json.Unmarshal(native.Message.Content, &text) == nil {
		r.Text = text
		return r, nil
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(native.Message.Content, &blocks); err != nil {
		return Record{}, fmt.Errorf("unsupported Claude message content: %w", err)
	}
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" {
			parts = append(parts, b.Text)
		}
	}
	r.Text = strings.Join(parts, "\n")
	return r, nil
}
