package claude

import "testing"

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
