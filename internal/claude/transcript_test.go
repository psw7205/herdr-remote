package claude

import (
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
