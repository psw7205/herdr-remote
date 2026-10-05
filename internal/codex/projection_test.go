package codex

import "testing"

func TestProjectionIsLinearAndSkipsHiddenRecords(t *testing.T) {
	p := NewProjection()
	for _, r := range []Record{
		{ID: "1", Role: "user"},
		{ID: "2", Role: "user", Text: "question"},
		{ID: "3", Role: "assistant", Text: "progress"},
		{},
		{ID: "5", Role: "assistant", Text: "answer"},
	} {
		if err := p.Add(r); err != nil {
			t.Fatal(err)
		}
	}
	got := p.Messages()
	if len(got) != 3 || got[0].ID != "2" || got[1].ID != "3" || got[2].ID != "5" {
		t.Fatalf("projection %+v", got)
	}
}

func TestProjectionIgnoresRepeatAndRejectsConflict(t *testing.T) {
	p := NewProjection()
	p.Add(Record{ID: "1", Role: "user", Text: "q"})
	p.Add(Record{ID: "2", Role: "assistant", Text: "a"})
	if err := p.Add(Record{ID: "1", Role: "user", Text: "q"}); err != nil {
		t.Fatal(err)
	}
	if got := p.Messages(); len(got) != 2 {
		t.Fatalf("repeat duplicated: %+v", got)
	}
	if err := p.Add(Record{ID: "1", Role: "user", Text: "changed"}); err == nil {
		t.Fatal("conflicting ordinal accepted")
	}
}
