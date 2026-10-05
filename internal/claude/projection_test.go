package claude

import (
	"slices"
	"testing"
)

func TestProjectionFollowsCurrentBranchThroughTools(t *testing.T) {
	p := NewProjection()
	for _, r := range []Record{
		{ID: "u", Role: "user", Text: "question"},
		{ID: "a", ParentID: "u", Role: "assistant", Text: "old answer"},
		{ID: "tool", ParentID: "a", Role: "user"},
		{ID: "next", ParentID: "tool", Role: "assistant", Text: "next answer"},
	} {
		if err := p.Add(r); err != nil {
			t.Fatal(err)
		}
	}
	if got := p.Messages(); len(got) != 3 || got[2].ID != "next" {
		t.Fatalf("wrong projection: %+v", got)
	}
	if err := p.Add(Record{ID: "branch", ParentID: "u", Role: "assistant", Text: "revised"}); err != nil {
		t.Fatal(err)
	}
	if got := p.Messages(); len(got) != 2 || got[1].ID != "branch" {
		t.Fatalf("mixed branches: %+v", got)
	}
}
func TestDuplicateRecordDoesNotSwitchLeaf(t *testing.T) {
	p := NewProjection()
	p.Add(Record{ID: "u", Role: "user", Text: "q"})
	p.Add(Record{ID: "a", ParentID: "u", Role: "assistant", Text: "a"})
	p.Add(Record{ID: "u", Role: "user", Text: "q"})
	if got := p.Messages(); len(got) != 2 {
		t.Fatalf("duplicate moved leaf: %+v", got)
	}
}
func TestConflictingUUIDAndCycleAreRejected(t *testing.T) {
	p := NewProjection()
	p.Add(Record{ID: "u", Role: "user", Text: "q"})
	if err := p.Add(Record{ID: "u", Role: "user", Text: "changed"}); err == nil {
		t.Fatal("conflicting UUID accepted")
	}
	if err := p.Add(Record{ID: "a", ParentID: "a"}); err == nil {
		t.Fatal("cycle accepted")
	}
}

// Claude Code writes one content block per assistant record once the API
// message completes. Records of one message.id chain linearly, and parallel
// tool results land between the tool_use records of the same message.
func TestProjectionOfPerBlockRecords(t *testing.T) {
	lines := []string{
		`{"type":"user","uuid":"u","parentUuid":null,"sessionId":"s1","message":{"role":"user","content":"question"}}`,
		`{"type":"attachment","uuid":"att","parentUuid":"u","sessionId":"s1"}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"att","sessionId":"s1","message":{"role":"assistant","id":"m1","stop_reason":"tool_use","content":[{"type":"thinking","thinking":""}]}}`,
		`{"type":"assistant","uuid":"a2","parentUuid":"a1","sessionId":"s1","message":{"role":"assistant","id":"m1","stop_reason":"tool_use","content":[{"type":"text","text":"before tools"}]}}`,
		`{"type":"assistant","uuid":"a3","parentUuid":"a2","sessionId":"s1","message":{"role":"assistant","id":"m1","stop_reason":"tool_use","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{}}]}}`,
		`{"type":"user","uuid":"r1","parentUuid":"a3","sessionId":"s1","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"out"}]}}`,
		`{"type":"assistant","uuid":"a4","parentUuid":"r1","sessionId":"s1","message":{"role":"assistant","id":"m1","stop_reason":"tool_use","content":[{"type":"tool_use","id":"t2","name":"Bash","input":{}}]}}`,
		`{"type":"user","uuid":"r2","parentUuid":"a4","sessionId":"s1","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t2","content":"out"}]}}`,
		`{"type":"assistant","uuid":"b1","parentUuid":"r2","sessionId":"s1","message":{"role":"assistant","id":"m2","stop_reason":"end_turn","content":[{"type":"thinking","thinking":""}]}}`,
		`{"type":"assistant","uuid":"b2","parentUuid":"b1","sessionId":"s1","message":{"role":"assistant","id":"m2","stop_reason":"end_turn","content":[{"type":"text","text":"after tools"}]}}`,
		`{"type":"system","uuid":"sys","parentUuid":"b2","sessionId":"s1"}`,
	}
	p := NewProjection()
	for _, line := range lines {
		r, err := Decode([]byte(line), "s1")
		if err != nil {
			t.Fatal(err)
		}
		if err := p.Add(r); err != nil {
			t.Fatal(err)
		}
	}
	// Text messages keep their IDs and order; each call sits at its place.
	var got []string
	for _, m := range p.Messages() {
		got = append(got, m.ID+":"+m.Role+":"+m.Text+m.Tool.State)
	}
	want := []string{"u:user:question", "a2:assistant:before tools", "a3/t1:tool:completed", "a4/t2:tool:completed", "b2:assistant:after tools"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}
