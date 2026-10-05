package claude

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/psw7205/herdr-remote/internal/transcript"
)

// toolDetailBytes caps a tool's input and its result separately. Every Chat
// snapshot carries both for every call, so the cap bounds a long session's
// snapshot; the full text stays in the Terminal and the native transcript.
const toolDetailBytes = 8 << 10

const summaryRunes = 200

// Tool blocks are parsed leniently: a block that does not fit is dropped or
// shown without detail, and never invalidates the transcript.
func decodeToolCall(raw json.RawMessage, cwd string) (transcript.ToolCall, bool) {
	var b struct {
		ID    json.RawMessage `json:"id"`
		Name  json.RawMessage `json:"name"`
		Input json.RawMessage `json:"input"`
	}
	if json.Unmarshal(raw, &b) != nil {
		return transcript.ToolCall{}, false
	}
	call := transcript.ToolCall{ID: lenientString(b.ID), Name: lenientString(b.Name)}
	if call.ID == "" || call.Name == "" {
		return transcript.ToolCall{}, false
	}
	var pretty bytes.Buffer
	if json.Indent(&pretty, b.Input, "", "  ") == nil && pretty.String() != "null" {
		call.Input, call.InputTruncated = capText(pretty.String())
	}
	call.Summary = summarize(call.Name, b.Input, cwd)
	return call, true
}

func decodeToolResult(raw json.RawMessage) (transcript.ToolResult, bool) {
	var b struct {
		ToolUseID json.RawMessage `json:"tool_use_id"`
		Content   json.RawMessage `json:"content"`
		IsError   json.RawMessage `json:"is_error"`
	}
	if json.Unmarshal(raw, &b) != nil {
		return transcript.ToolResult{}, false
	}
	result := transcript.ToolResult{ToolUseID: lenientString(b.ToolUseID)}
	if result.ToolUseID == "" {
		return transcript.ToolResult{}, false
	}
	_ = json.Unmarshal(b.IsError, &result.Error)
	result.Text, result.Truncated = capText(resultText(b.Content))
	return result, true
}

// resultText keeps text blocks and names every other block, so an image or
// any binary payload is never copied into Chat.
func resultText(content json.RawMessage) string {
	var text string
	if json.Unmarshal(content, &text) == nil {
		return text
	}
	var blocks []json.RawMessage
	if json.Unmarshal(content, &blocks) != nil {
		return ""
	}
	parts := make([]string, 0, len(blocks))
	for _, raw := range blocks {
		var b struct {
			Type json.RawMessage `json:"type"`
			Text json.RawMessage `json:"text"`
		}
		if json.Unmarshal(raw, &b) != nil {
			continue
		}
		switch kind := lenientString(b.Type); kind {
		case "text":
			parts = append(parts, lenientString(b.Text))
		case "":
		default:
			parts = append(parts, "["+kind+"]")
		}
	}
	return strings.Join(parts, "\n")
}

// summarize names what a well-known tool acted on in one line. Other tools,
// including MCP tools, show their name only.
func summarize(name string, input json.RawMessage, cwd string) string {
	var fields map[string]json.RawMessage
	if json.Unmarshal(input, &fields) != nil {
		return ""
	}
	field := func(key string) string { return lenientString(fields[key]) }
	var summary string
	switch name {
	case "Bash":
		summary = firstLine(field("command"))
	case "Read", "Write", "Edit", "MultiEdit":
		summary = relative(field("file_path"), cwd)
	case "NotebookEdit":
		summary = relative(field("notebook_path"), cwd)
	case "Grep", "Glob":
		summary = field("pattern")
	case "Task", "Agent":
		summary = field("description")
	case "WebFetch":
		if u, err := url.Parse(field("url")); err == nil {
			summary = u.Host
		}
	case "WebSearch", "ToolSearch":
		summary = field("query")
	case "Skill":
		summary = field("skill")
	case "TodoWrite":
		var todos []json.RawMessage
		if json.Unmarshal(fields["todos"], &todos) == nil {
			summary = fmt.Sprintf("%d todos", len(todos))
		}
	}
	return oneLine(summary)
}

func lenientString(raw json.RawMessage) string {
	var s string
	_ = json.Unmarshal(raw, &s)
	return s
}

func firstLine(text string) string {
	for line := range strings.Lines(text) {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

func oneLine(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(text) <= summaryRunes {
		return text
	}
	return string([]rune(text)[:summaryRunes-1]) + "…"
}

// relative shows a path inside the record's working directory relative to it.
func relative(path, cwd string) string {
	if cwd == "" || !filepath.IsAbs(path) {
		return path
	}
	rel, err := filepath.Rel(cwd, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
		return path
	}
	return rel
}

func capText(text string) (string, bool) {
	if len(text) <= toolDetailBytes {
		return text, false
	}
	cut := toolDetailBytes
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut], true
}
