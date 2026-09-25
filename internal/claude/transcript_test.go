package claude

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestDecodeTextAndParentIdentity(t *testing.T) {
	r, err := Decode([]byte(`{"type":"user","uuid":"u1","parentUuid":null,"sessionId":"s1","timestamp":"2026-09-22T00:00:00Z","message":{"role":"user","content":"hello"}}`), "s1")
	if err != nil {
		t.Fatal(err)
	}
	if r.ID != "u1" || r.Text != "hello" || r.Role != "user" {
		t.Fatalf("wrong record: %+v", r)
	}
}
func TestDecodeAssistantBlocks(t *testing.T) {
	r, err := Decode([]byte(`{"type":"assistant","uuid":"a1","parentUuid":"u1","sessionId":"s1","message":{"role":"assistant","id":"m1","content":[{"type":"thinking","thinking":"private"},{"type":"text","text":"answer"},{"type":"tool_use","name":"Bash","input":{}}]}}`), "s1")
	if err != nil {
		t.Fatal(err)
	}
	if r.Text != "answer" || r.ParentID != "u1" {
		t.Fatalf("wrong text: %+v", r)
	}
}
func TestToolResultIsNotUserPrompt(t *testing.T) {
	r, err := Decode([]byte(`{"type":"user","uuid":"tool1","parentUuid":"a1","sessionId":"s1","message":{"role":"user","content":[{"type":"tool_result","content":"output"}]}}`), "s1")
	if err != nil {
		t.Fatal(err)
	}
	if r.Text != "" || r.ID != "tool1" {
		t.Fatalf("lost graph or exposed tool result: %+v", r)
	}
}
func TestRejectsMixedSession(t *testing.T) {
	_, err := Decode([]byte(`{"type":"user","uuid":"u1","sessionId":"other","message":{"role":"user","content":"wrong"}}`), "s1")
	if !errors.Is(err, ErrSessionMismatch) {
		t.Fatalf("got %v", err)
	}
}
func TestIgnoresSidechainsAndUnknownContent(t *testing.T) {
	for _, s := range []string{
		`{"type":"assistant","uuid":"a1","sessionId":"s1","isSidechain":true,"message":{"role":"assistant","content":"subagent"}}`,
		`{"type":"future","sessionId":"s1"}`,
		`{"type":"assistant","uuid":"a1","sessionId":"s1","message":{"role":"assistant","content":[{"type":"future","text":"unknown"}]}}`,
	} {
		r, err := Decode([]byte(s), "s1")
		if err != nil {
			t.Fatal(err)
		}
		if r.Text != "" {
			t.Fatalf("exposed unsupported content: %+v", r)
		}
	}
}
func TestRejectsMalformedRecord(t *testing.T) {
	if _, err := Decode([]byte(`{"type":`), "s1"); err == nil {
		t.Fatal("malformed JSON accepted")
	}
}
func userLine(t *testing.T, content any, meta bool) []byte {
	t.Helper()
	line, err := json.Marshal(map[string]any{"type": "user", "uuid": "c1", "parentUuid": "a1", "sessionId": "s1", "isMeta": meta, "message": map[string]any{"role": "user", "content": content}})
	if err != nil {
		t.Fatal(err)
	}
	return line
}
func TestHidesMetaRecordsButKeepsGraph(t *testing.T) {
	for _, content := range []any{
		"<local-command-caveat>notice</local-command-caveat>",
		"plain meta prompt",
		[]map[string]string{{"type": "text", "text": "Base directory for this skill: /skills/sample\n\nskill body"}},
	} {
		r, err := Decode(userLine(t, content, true), "s1")
		if err != nil {
			t.Fatal(err)
		}
		if r.Text != "" || r.ID != "c1" || r.ParentID != "a1" || r.Role != "user" {
			t.Fatalf("meta record exposed or graph lost: %+v", r)
		}
	}
}
func TestHidesLocalCommandRecords(t *testing.T) {
	for _, content := range []string{
		"<command-name>/sample</command-name>\n<command-message>sample</command-message>\n<command-args></command-args>",
		"<command-message>sample is running</command-message>\n<command-name>/sample</command-name>",
		"<command-args>value</command-args>",
		"<local-command-stdout>output</local-command-stdout>",
		"<local-command-stderr>error</local-command-stderr>",
		"<local-command-caveat>notice</local-command-caveat>",
		"<bash-input>ls</bash-input>",
		"<bash-stdout>out</bash-stdout><bash-stderr></bash-stderr>",
		"<bash-stderr>error</bash-stderr>",
		"\n  <command-name>/sample</command-name>",
		"<task-notification>\n<task-id>t1</task-id>\n<status>completed</status>\n</task-notification>",
	} {
		r, err := Decode(userLine(t, content, false), "s1")
		if err != nil {
			t.Fatal(err)
		}
		if r.Text != "" || r.ID != "c1" || r.ParentID != "a1" {
			t.Fatalf("local command exposed or graph lost for %q: %+v", content, r)
		}
	}
}
func TestStripsOnlyPastedContentWrapper(t *testing.T) {
	// Native transcripts repeat the id attribute on the closing tag.
	for content, want := range map[string]string{
		"\n\n<pasted_content id=\"p1\">\npasted line\n</pasted_content id=\"p1\">":                    "\n\n\npasted line\n",
		"typed text\n\n\n<pasted_content id=\"p2\">\npasted\n</pasted_content id=\"p2\">":             "typed text\n\n\n\npasted\n",
		"<pasted_content id=\"p3\">pasted</pasted_content> typed after":                               "pasted typed after",
		"\n\n<pasted_content id=\"p4\">\n<bash-stdout>log</bash-stdout>\n</pasted_content id=\"p4\">": "\n\n\n<bash-stdout>log</bash-stdout>\n",
	} {
		r, err := Decode(userLine(t, content, false), "s1")
		if err != nil {
			t.Fatal(err)
		}
		if r.Text != want {
			t.Fatalf("pasted prompt %q became %q, want %q", content, r.Text, want)
		}
	}
}
func TestUnknownMarkersStayVisible(t *testing.T) {
	for _, content := range []any{
		"<future-marker>kept</future-marker>",
		"question about <command-name> tags",
		// A typed prompt that merely starts with a marker element is still the user's.
		"<bash-stderr>permission denied</bash-stderr> 원인?",
		"<command-name>/sample",
		[]map[string]string{{"type": "text", "text": "<local-command-stdout>log</local-command-stdout>"}, {"type": "text", "text": "이 출력 설명해 줘"}},
		[]map[string]string{{"type": "text", "text": "[Request interrupted by user]"}},
	} {
		r, err := Decode(userLine(t, content, false), "s1")
		if err != nil {
			t.Fatal(err)
		}
		if r.Text == "" {
			t.Fatalf("unknown form hidden: %v", content)
		}
	}
	r, err := Decode([]byte(`{"type":"assistant","uuid":"a1","sessionId":"s1","message":{"role":"assistant","model":"<synthetic>","content":[{"type":"text","text":"API Error: sample"}]}}`), "s1")
	if err != nil || r.Text != "API Error: sample" {
		t.Fatalf("synthetic assistant record hidden: %+v %v", r, err)
	}
}
func TestHiddenRecordKeepsParentChain(t *testing.T) {
	p := NewProjection()
	for _, s := range []string{
		`{"type":"user","uuid":"u1","parentUuid":null,"sessionId":"s1","message":{"role":"user","content":"question"}}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"u1","sessionId":"s1","message":{"role":"assistant","content":[{"type":"text","text":"answer"}]}}`,
		`{"type":"user","uuid":"c1","parentUuid":"a1","sessionId":"s1","message":{"role":"user","content":"<command-name>/sample</command-name>"}}`,
		`{"type":"user","uuid":"o1","parentUuid":"c1","sessionId":"s1","message":{"role":"user","content":"<local-command-stdout>done</local-command-stdout>"}}`,
		`{"type":"user","uuid":"m1","parentUuid":"o1","sessionId":"s1","isMeta":true,"message":{"role":"user","content":"meta prompt"}}`,
		`{"type":"assistant","uuid":"a2","parentUuid":"m1","sessionId":"s1","message":{"role":"assistant","content":[{"type":"text","text":"next answer"}]}}`,
	} {
		r, err := Decode([]byte(s), "s1")
		if err != nil {
			t.Fatal(err)
		}
		if err := p.Add(r); err != nil {
			t.Fatal(err)
		}
	}
	got := p.Messages()
	if len(got) != 3 || got[0].ID != "u1" || got[1].ID != "a1" || got[2].ID != "a2" {
		t.Fatalf("hidden records broke the chain or leaked: %+v", got)
	}
}
