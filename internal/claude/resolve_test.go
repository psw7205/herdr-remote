package claude

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveUsesNativeIDNotCWDOrModificationTime(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "projects", "same-cwd")
	os.MkdirAll(dir, 0700)
	a := "11111111-1111-4111-8111-111111111111"
	b := "22222222-2222-4222-8222-222222222222"
	os.WriteFile(filepath.Join(dir, a+".jsonl"), nil, 0600)
	os.WriteFile(filepath.Join(dir, b+".jsonl"), nil, 0600)
	path, err := Resolve(root, a)
	if err != nil || filepath.Base(path) != a+".jsonl" {
		t.Fatal(path, err)
	}
}
func TestResolveRejectsAmbiguousAndTraversal(t *testing.T) {
	root := t.TempDir()
	id := "11111111-1111-4111-8111-111111111111"
	for _, part := range []string{"a", "b"} {
		dir := filepath.Join(root, "projects", part)
		os.MkdirAll(dir, 0700)
		os.WriteFile(filepath.Join(dir, id+".jsonl"), nil, 0600)
	}
	if _, e := Resolve(root, id); e == nil || errors.Is(e, ErrNoTranscript) {
		t.Fatalf("ambiguous transcript accepted or reported as missing: %v", e)
	}
	if _, e := Resolve(root, "../../secret"); e == nil || errors.Is(e, ErrNoTranscript) {
		t.Fatalf("traversal accepted or reported as missing: %v", e)
	}
}
func TestResolveReportsMissingTranscriptDistinctly(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "projects", "repo"), 0700)
	if _, e := Resolve(root, "33333333-3333-4333-8333-333333333333"); !errors.Is(e, ErrNoTranscript) {
		t.Fatalf("missing transcript not reported: %v", e)
	}
}
