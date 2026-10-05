package claude

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/psw7205/herdr-remote/internal/transcript"
)

// Synthetic records shaped like Claude Code's: one content block per
// assistant record, each tool_result in its own user record.
func line(t *testing.T, kind, id, parent, turn string, content any) string {
	t.Helper()
	message := map[string]any{"role": kind, "content": content}
	if turn != "" {
		message["id"] = turn
	}
	record := map[string]any{"type": kind, "uuid": id, "sessionId": "s1", "cwd": "/work/repo", "timestamp": "2026-10-05T00:00:00Z", "message": message}
	if parent != "" {
		record["parentUuid"] = parent
	}
	b, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func use(t *testing.T, id, parent, turn, toolID, name string, input any) string {
	return line(t, "assistant", id, parent, turn, []map[string]any{{"type": "tool_use", "id": toolID, "name": name, "input": input}})
}
func result(t *testing.T, id, parent, toolID string, content any, isError bool) string {
	block := map[string]any{"type": "tool_result", "tool_use_id": toolID, "content": content}
	if isError {
		block["is_error"] = true
	}
	return line(t, "user", id, parent, "", []map[string]any{block})
}
func project(t *testing.T, p *Projection, lines ...string) []Record {
	t.Helper()
	for _, l := range lines {
		r, err := Decode([]byte(l), "s1")
		if err != nil {
			t.Fatalf("decode %s: %v", l, err)
		}
		if err := p.Add(r); err != nil {
			t.Fatal(err)
		}
	}
	return p.Messages()
}
func tools(messages []Record) map[string]transcript.Tool {
	out := make(map[string]transcript.Tool)
	for _, m := range messages {
		if m.Role == "tool" {
			out[m.Tool.ID] = m.Tool
		}
	}
	return out
}

func TestToolRunsUntilItsResultArrives(t *testing.T) {
	p := NewProjection()
	got := project(t, p,
		line(t, "user", "u", "", "", "list files"),
		use(t, "a1", "u", "m1", "t1", "Bash", map[string]any{"command": "ls -la\nsecond line", "description": "List"}),
	)
	if len(got) != 2 || got[1].ID != "a1/t1" || got[1].Role != "tool" || got[1].Text != "" {
		t.Fatalf("wrong items: %+v", got)
	}
	tool := got[1].Tool
	if tool.State != transcript.ToolRunning || tool.Name != "Bash" || tool.Summary != "ls -la" || tool.Result != "" {
		t.Fatalf("wrong running tool: %+v", tool)
	}
	if !strings.Contains(tool.Input, "\"command\": \"ls -la\\nsecond line\"") || !strings.Contains(tool.Input, "\n  ") {
		t.Fatalf("input not pretty JSON: %q", tool.Input)
	}
	got = project(t, p, result(t, "r1", "a1", "t1", "file-a\nfile-b", false))
	if tool := tools(got)["t1"]; tool.State != transcript.ToolCompleted || tool.Result != "file-a\nfile-b" || got[1].ID != "a1/t1" {
		t.Fatalf("result not paired: %+v", got)
	}
}

func TestParallelToolsInterleaveWithinOneMessage(t *testing.T) {
	p := NewProjection()
	got := project(t, p,
		line(t, "user", "u", "", "", "q"),
		use(t, "a1", "u", "m1", "tA", "Read", map[string]any{"file_path": "/work/repo/src/a.ts"}),
		result(t, "r1", "a1", "tA", "a", false),
		use(t, "a2", "r1", "m1", "tB", "Grep", map[string]any{"pattern": "TODO"}),
	)
	byID := tools(got)
	if byID["tA"].State != transcript.ToolCompleted || byID["tB"].State != transcript.ToolRunning {
		t.Fatalf("wrong parallel states: %+v", byID)
	}
	if got[1].Tool.ID != "tA" || got[2].Tool.ID != "tB" {
		t.Fatalf("tools out of transcript order: %+v", got)
	}
	got = project(t, p,
		result(t, "r2", "a2", "tB", "", false),
		line(t, "assistant", "b1", "r2", "m2", []map[string]any{{"type": "text", "text": "done"}}),
	)
	if byID := tools(got); byID["tB"].State != transcript.ToolCompleted || got[len(got)-1].Text != "done" {
		t.Fatalf("second result not paired: %+v", got)
	}
}

func TestErrorResultAndUnmatchedResult(t *testing.T) {
	p := NewProjection()
	got := project(t, p,
		line(t, "user", "u", "", "", "q"),
		use(t, "a1", "u", "m1", "t1", "Edit", map[string]any{"file_path": "/elsewhere/x.go"}),
		result(t, "r1", "a1", "t1", "<tool_use_error>File has not been read yet.</tool_use_error>", true),
		result(t, "r2", "r1", "t-missing", "orphan", false),
	)
	if len(got) != 2 {
		t.Fatalf("orphan result became an item: %+v", got)
	}
	if tool := got[1].Tool; tool.State != transcript.ToolError || tool.Summary != "/elsewhere/x.go" || !strings.Contains(tool.Result, "not been read") {
		t.Fatalf("wrong error tool: %+v", tool)
	}
}

func TestCallWithoutResultBecomesUnknownOnlyOnceTheChainMovesOn(t *testing.T) {
	p := NewProjection()
	base := []string{
		line(t, "user", "u", "", "", "q"),
		use(t, "a1", "u", "m1", "t1", "Bash", map[string]any{"command": "sleep 100"}),
	}
	if tools(project(t, p, base...))["t1"].State != transcript.ToolRunning {
		t.Fatal("call without result is not running")
	}
	// A later record of the same API message keeps the call running.
	if tools(project(t, p, use(t, "a2", "a1", "m1", "t2", "Bash", map[string]any{"command": "true"})))["t1"].State != transcript.ToolRunning {
		t.Fatal("same message ended the call")
	}
	// The next prompt (here the interrupt notice) ends it without a result.
	got := project(t, p, line(t, "user", "u2", "a2", "", []map[string]any{{"type": "text", "text": "[Request interrupted by user]"}}))
	if byID := tools(got); byID["t1"].State != transcript.ToolUnknown || byID["t2"].State != transcript.ToolUnknown {
		t.Fatalf("abandoned calls not unknown: %+v", byID)
	}

	q := NewProjection()
	got = project(t, q, append(base, line(t, "assistant", "b1", "a1", "m2", []map[string]any{{"type": "text", "text": "next message"}}))...)
	if tools(got)["t1"].State != transcript.ToolUnknown {
		t.Fatal("another API message did not end the call")
	}
}

func TestResultOnAnotherBranchIsNotPaired(t *testing.T) {
	p := NewProjection()
	got := project(t, p,
		line(t, "user", "u", "", "", "q"),
		use(t, "a1", "u", "m1", "t1", "Bash", map[string]any{"command": "make"}),
		result(t, "r1", "a1", "t1", "built", false),
		use(t, "a2", "a1", "m1", "t2", "Bash", map[string]any{"command": "make test"}),
	)
	if byID := tools(got); byID["t1"].State != transcript.ToolRunning || byID["t2"].State != transcript.ToolRunning {
		t.Fatalf("result from another branch used: %+v", byID)
	}
}

func TestMalformedToolBlocksNeverInvalidateTheTranscript(t *testing.T) {
	p := NewProjection()
	got := project(t, p,
		line(t, "user", "u", "", "", "q"),
		line(t, "assistant", "a1", "u", "m1", []map[string]any{{"type": "tool_use", "id": 7, "name": "Bash", "input": map[string]any{}}}),
		line(t, "assistant", "a2", "a1", "m1", []map[string]any{{"type": "tool_use", "id": "t2", "name": []int{1}}}),
		line(t, "assistant", "a3", "a2", "m1", []map[string]any{{"type": "tool_use", "id": "t3", "name": "Bash", "input": "not an object"}}),
		line(t, "assistant", "a4", "a3", "m1", []map[string]any{{"type": "tool_use", "id": "t4", "name": "Read"}}),
		line(t, "user", "r3", "a4", "", []map[string]any{{"type": "tool_result", "tool_use_id": "t3", "content": 42, "is_error": "yes"}}),
		line(t, "user", "r4", "r3", "", []map[string]any{{"type": "tool_result", "tool_use_id": []string{"t4"}, "content": "x"}}),
		line(t, "assistant", "a5", "r4", "m1", []map[string]any{{"type": "server_tool_use", "id": "s1", "name": "web_search", "input": map[string]any{}}}),
		line(t, "assistant", "b1", "a5", "m2", []map[string]any{{"type": "text", "text": "still readable"}}),
	)
	var ids []string
	for _, m := range got {
		ids = append(ids, m.ID)
	}
	if strings.Join(ids, ",") != "u,a3/t3,a4/t4,b1" {
		t.Fatalf("wrong items: %v", ids)
	}
	byID := tools(got)
	if t3 := byID["t3"]; t3.State != transcript.ToolCompleted || t3.Result != "" || t3.Summary != "" || t3.Input != "\"not an object\"" {
		t.Fatalf("lenient result wrong: %+v", t3)
	}
	if t4 := byID["t4"]; t4.State != transcript.ToolUnknown || t4.Input != "" || t4.Summary != "" {
		t.Fatalf("call without input wrong: %+v", t4)
	}
}

func TestToolDetailIsCappedAndNeverCarriesImages(t *testing.T) {
	big := strings.Repeat("가", toolDetailBytes)
	p := NewProjection()
	got := project(t, p,
		line(t, "user", "u", "", "", "q"),
		use(t, "a1", "u", "m1", "t1", "Write", map[string]any{"file_path": "/work/repo/notes.md", "content": big}),
		result(t, "r1", "a1", "t1", []map[string]any{
			{"type": "text", "text": big},
		}, false),
		use(t, "a2", "r1", "m1", "t2", "Read", map[string]any{"file_path": "/work/repo/shot.png"}),
		result(t, "r2", "a2", "t2", []map[string]any{
			{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "data": "iVBORw0KGgoAAAANSUhEUg"}},
			{"type": "text", "text": "caption"},
		}, false),
	)
	byID := tools(got)
	write := byID["t1"]
	if !write.InputTruncated || !write.ResultTruncated || len(write.Input) > toolDetailBytes || len(write.Result) > toolDetailBytes {
		t.Fatalf("detail not capped: %d %d %v %v", len(write.Input), len(write.Result), write.InputTruncated, write.ResultTruncated)
	}
	if !utf8.ValidString(write.Input) || !utf8.ValidString(write.Result) || write.Summary != "notes.md" {
		t.Fatalf("cut inside a rune or wrong summary: %q", write.Summary)
	}
	if read := byID["t2"]; read.Result != "[image]\ncaption" || read.ResultTruncated || strings.Contains(read.Result, "iVBOR") {
		t.Fatalf("image leaked or lost: %q", read.Result)
	}
}

func TestSummaries(t *testing.T) {
	for _, c := range []struct {
		name  string
		input any
		want  string
	}{
		{"Bash", map[string]any{"command": "\n\n  pnpm test -- --run\nnext"}, "pnpm test -- --run"},
		{"Read", map[string]any{"file_path": "/work/repo/src/app.ts"}, "src/app.ts"},
		{"Read", map[string]any{"file_path": "/work/repository/x.ts"}, "/work/repository/x.ts"},
		{"Edit", map[string]any{"file_path": "relative.go"}, "relative.go"},
		{"MultiEdit", map[string]any{"file_path": "/work/repo/a.go"}, "a.go"},
		{"NotebookEdit", map[string]any{"notebook_path": "/work/repo/n.ipynb"}, "n.ipynb"},
		{"Grep", map[string]any{"pattern": "func\\s+Test", "path": "/work/repo"}, "func\\s+Test"},
		{"Glob", map[string]any{"pattern": "**/*.go"}, "**/*.go"},
		{"Task", map[string]any{"description": "Find callers", "prompt": "long"}, "Find callers"},
		{"Agent", map[string]any{"description": "Review diff"}, "Review diff"},
		{"WebFetch", map[string]any{"url": "https://docs.example.com/path?q=1"}, "docs.example.com"},
		{"WebSearch", map[string]any{"query": "go 1.25 release"}, "go 1.25 release"},
		{"ToolSearch", map[string]any{"query": "select:Read", "max_results": 1}, "select:Read"},
		{"Skill", map[string]any{"skill": "plan", "args": "x"}, "plan"},
		{"TodoWrite", map[string]any{"todos": []map[string]string{{"content": "a"}, {"content": "b"}, {"content": "c"}}}, "3 todos"},
		{"mcp__server__lookup", map[string]any{"query": "x"}, ""},
		{"Bash", map[string]any{"command": strings.Repeat("x", 300)}, strings.Repeat("x", summaryRunes-1) + "…"},
		{"Grep", map[string]any{"pattern": 5}, ""},
	} {
		got := project(t, NewProjection(), use(t, "a1", "", "m1", "t1", c.name, c.input))
		if len(got) != 1 || got[0].Tool.Summary != c.want || got[0].Tool.Name != c.name {
			t.Errorf("%s %v: got %+v, want summary %q", c.name, c.input, got, c.want)
		}
	}
}
