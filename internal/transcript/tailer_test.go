package transcript

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPartialAppendAndDuplicateRead(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	os.WriteFile(p, []byte("one\npar"), 0600)
	r := NewTailer(p)
	b, err := r.Read()
	if err != nil || len(b.Lines) != 1 || string(b.Lines[0]) != "one" || !b.Reset {
		t.Fatalf("first: %+v %v", b, err)
	}
	b, err = r.Read()
	if err != nil || len(b.Lines) != 0 || b.Reset {
		t.Fatalf("duplicate: %+v %v", b, err)
	}
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0600)
	f.WriteString("tial\ntwo\n")
	f.Close()
	b, err = r.Read()
	if err != nil || len(b.Lines) != 2 || string(b.Lines[0]) != "partial" || b.Reset {
		t.Fatalf("append: %+v %v", b, err)
	}
}
func TestTruncateReplacementAndSameSizeRewrite(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	os.WriteFile(p, []byte("long-line\n"), 0600)
	r := NewTailer(p)
	r.Read()
	os.WriteFile(p, []byte("new\n"), 0600)
	b, e := r.Read()
	if e != nil || !b.Reset || string(b.Lines[0]) != "new" {
		t.Fatalf("truncate %+v %v", b, e)
	}
	q := p + ".new"
	os.WriteFile(q, []byte("two\n"), 0600)
	os.Rename(q, p)
	b, e = r.Read()
	if e != nil || !b.Reset || string(b.Lines[0]) != "two" {
		t.Fatalf("replacement %+v %v", b, e)
	}
	os.WriteFile(p, []byte("one\n"), 0600)
	b, e = r.Read()
	if e != nil || !b.Reset || string(b.Lines[0]) != "one" {
		t.Fatalf("rewrite %+v %v", b, e)
	}
}
func TestMissingFileAndRecreation(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	r := NewTailer(p)
	if _, e := r.Read(); e == nil {
		t.Fatal("missing hidden")
	}
	os.WriteFile(p, []byte("x\n"), 0600)
	b, e := r.Read()
	if e != nil || !b.Reset {
		t.Fatal(b, e)
	}
}
