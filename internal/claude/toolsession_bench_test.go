package claude

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/psw7205/herdr-remote/internal/transcript"
)

var toolSessionGrid = []struct{ tools, result int }{
	{100, 200}, {1000, 200}, {5000, 200},
	{100, 8192}, {1000, 8192}, {5000, 8192},
}

// syntheticTool returns the records of tool call i in an anonymous sequential
// session shaped like Claude Code's: a prompt every 50 calls, a text block
// every 4, then the tool_use and its tool_result in records of their own, one
// API message per call. The result record also repeats the output in a
// toolUseResult field, an assumed native shape not checked against real
// transcripts; Decode ignores it, so it only adds transcript bytes. JSON is
// written without HTML escaping. internal/session keeps a copy of this
// generator so both packages measure the same shape.
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

// syntheticResult is ASCII command output whose lines each hold a newline,
// two tabs and a '>', which JSON escapes differently.
func syntheticResult(size int) string {
	const line = "ok  \texample.com/repo/internal/pkg\t0.012s -> synthetic output\n"
	return strings.Repeat(line, size/len(line)+1)[:size]
}

func syntheticLines(session string, tools, resultBytes int) ([][]byte, int) {
	result := syntheticResult(resultBytes)
	var lines [][]byte
	size := 0
	for i := range tools {
		for _, line := range syntheticTool(session, i, result) {
			lines = append(lines, line)
			size += len(line) + 1
		}
	}
	return lines, size
}

func projectLines(tb testing.TB, lines [][]byte) *Projection {
	tb.Helper()
	p := NewProjection()
	for _, line := range lines {
		r, err := Decode(line, "s1")
		if err != nil {
			tb.Fatal(err)
		}
		if err := p.Add(r); err != nil {
			tb.Fatal(err)
		}
	}
	return p
}

func TestSyntheticToolSessionCompletesEveryCall(t *testing.T) {
	for _, size := range []int{200, 8192, 8193} {
		lines, _ := syntheticLines("s1", 60, size)
		var calls, prompts, texts int
		for _, m := range projectLines(t, lines).Messages() {
			switch m.Role {
			case "tool":
				calls++
				if m.Tool.State != transcript.ToolCompleted || len(m.Tool.Result) != min(size, toolDetailBytes) || m.Tool.ResultTruncated != (size > toolDetailBytes) || !strings.HasPrefix(m.Tool.Summary, "go test ./internal/pkg") {
					t.Fatalf("size %d: wrong tool item: %+v", size, m.Tool)
				}
			case "user":
				prompts++
			case "assistant":
				texts++
			}
		}
		if calls != 60 || prompts != 2 || texts != 15 {
			t.Fatalf("size %d: %d calls, %d prompts, %d texts", size, calls, prompts, texts)
		}
	}
}

func BenchmarkToolSessionProject(b *testing.B) {
	for _, c := range toolSessionGrid {
		b.Run(fmt.Sprintf("tools=%d/result=%dB", c.tools, c.result), func(b *testing.B) {
			lines, size := syntheticLines("s1", c.tools, c.result)
			b.SetBytes(int64(size))
			var items int
			for b.Loop() {
				items = len(projectLines(b, lines).Messages())
			}
			b.ReportMetric(float64(size), "transcript-B")
			b.ReportMetric(float64(items), "items")
		})
	}
}

// Every live batch runs Messages over the whole branch.
func BenchmarkToolSessionMessages(b *testing.B) {
	for _, c := range toolSessionGrid {
		b.Run(fmt.Sprintf("tools=%d/result=%dB", c.tools, c.result), func(b *testing.B) {
			lines, _ := syntheticLines("s1", c.tools, c.result)
			p := projectLines(b, lines)
			for b.Loop() {
				p.Messages()
			}
		})
	}
}
