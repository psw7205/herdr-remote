// Package claude reads native JSONL records without writing conversation data.
package claude

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/psw7205/herdr-remote/internal/transcript"
	"regexp"
	"strings"
)

var ErrSessionMismatch = errors.New("transcript session identity mismatch")

// localCommandElements match the elements of the user records Claude Code
// writes itself instead of a typed prompt: slash commands, bash mode, their
// output, and notices of finished background tasks. A record is hidden only
// when it consists of these elements alone.
var localCommandElements = func() []*regexp.Regexp {
	var out []*regexp.Regexp
	for _, name := range []string{
		"command-name", "command-message", "command-args",
		"local-command-stdout", "local-command-stderr", "local-command-caveat",
		"bash-input", "bash-stdout", "bash-stderr",
		"task-notification",
	} {
		out = append(out, regexp.MustCompile(`(?s)<`+name+`(?:\s[^>]*)?>.*?</`+name+`>`))
	}
	return out
}()

// pastedContentTag matches both wrapper tags; native closing tags repeat the
// id attribute.
var pastedContentTag = regexp.MustCompile(`</?pasted_content(?:\s[^>]*)?>`)

// Record retains non-message graph nodes so filtering tool output does not
// break parent traversal. Text and tool calls are eligible for Chat rendering;
// tool results only complete a call.
type Record = transcript.Record

func Decode(line []byte, sessionID string) (Record, error) {
	var native struct {
		Type      string `json:"type"`
		ID        string `json:"uuid"`
		ParentID  string `json:"parentUuid"`
		SessionID string `json:"sessionId"`
		Sidechain bool   `json:"isSidechain"`
		Meta      bool   `json:"isMeta"`
		Timestamp string `json:"timestamp"`
		CWD       string `json:"cwd"`
		Message   struct {
			Role    string          `json:"role"`
			ID      string          `json:"id"`
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
	if r.Role == "assistant" {
		r.Turn = native.Message.ID
	}
	var text string
	if json.Unmarshal(native.Message.Content, &text) == nil {
		r.Text = visibleText(r.Role, text, native.Meta)
		return r, nil
	}
	var blocks []json.RawMessage
	if err := json.Unmarshal(native.Message.Content, &blocks); err != nil {
		return Record{}, fmt.Errorf("unsupported Claude message content: %w", err)
	}
	var parts []string
	for _, raw := range blocks {
		var b struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(raw, &b); err != nil {
			return Record{}, fmt.Errorf("unsupported Claude message content: %w", err)
		}
		switch {
		case b.Type == "text":
			parts = append(parts, b.Text)
		case b.Type == "tool_use" && r.Role == "assistant":
			if call, ok := decodeToolCall(raw, native.CWD); ok {
				r.Calls = append(r.Calls, call)
			}
		case b.Type == "tool_result" && r.Role == "user":
			if result, ok := decodeToolResult(raw); ok {
				r.Results = append(r.Results, result)
			}
		}
	}
	r.Text = visibleText(r.Role, strings.Join(parts, "\n"), native.Meta)
	return r, nil
}

// visibleText empties user records that Claude Code injects (isMeta) or
// writes for local commands; they remain graph nodes. Unknown forms, and any
// record with text beside the local-command elements, stay visible. The check
// runs before unwrapping, so a pasted log made of such elements is still shown.
func visibleText(role, text string, meta bool) string {
	if role != "user" {
		return text
	}
	if meta {
		return ""
	}
	if onlyLocalCommand(text) {
		return ""
	}
	return pastedContentTag.ReplaceAllString(text, "")
}

func onlyLocalCommand(text string) bool {
	rest := text
	for _, element := range localCommandElements {
		rest = element.ReplaceAllString(rest, "")
	}
	return rest != text && strings.TrimSpace(rest) == ""
}
