package codex

import (
	"os"
	"path/filepath"
	"testing"
)

func writeRollout(t *testing.T, root, day, stamp, id, first string) string {
	t.Helper()
	dir := filepath.Join(root, "sessions", day)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "rollout-"+stamp+"-"+id+".jsonl")
	if err := os.WriteFile(path, []byte(first), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func meta(id string) string {
	return `{"timestamp":"2026-10-01T00:00:00.000Z","type":"session_meta","ordinal":0,"payload":{"id":"` + id + `","source":"cli"}}` + "\n"
}

func TestResolveFindsRolloutByNativeID(t *testing.T) {
	root := t.TempDir()
	other := "01a0aaaa-0000-7000-8000-000000000002"
	want := writeRollout(t, root, "2026/10/01", "2026-10-01T00-00-00", session, meta(session))
	writeRollout(t, root, "2026/10/01", "2026-10-01T00-00-01", other, meta(other))
	path, err := Resolve(root, session)
	if err != nil || path != want {
		t.Fatal(path, err)
	}
}

func TestResolveRejectsMismatchedSessionMeta(t *testing.T) {
	root := t.TempDir()
	other := "01a0aaaa-0000-7000-8000-000000000002"
	writeRollout(t, root, "2026/10/01", "2026-10-01T00-00-00", session, meta(other))
	if _, err := Resolve(root, session); err == nil {
		t.Fatal("rollout of another session accepted")
	}
	root = t.TempDir()
	writeRollout(t, root, "2026/10/01", "2026-10-01T00-00-00", session, `{"timestamp":"x","type":"response_item","ordinal":0,"payload":{}}`+"\n")
	if _, err := Resolve(root, session); err == nil {
		t.Fatal("rollout without leading session_meta accepted")
	}
	root = t.TempDir()
	writeRollout(t, root, "2026/10/01", "2026-10-01T00-00-00", session, "")
	if _, err := Resolve(root, session); err == nil {
		t.Fatal("empty rollout accepted")
	}
}

func TestResolveRejectsAmbiguousArchivedAndInvalidIDs(t *testing.T) {
	root := t.TempDir()
	writeRollout(t, root, "2026/10/01", "2026-10-01T00-00-00", session, meta(session))
	writeRollout(t, root, "2026/10/02", "2026-10-02T00-00-00", session, meta(session))
	if _, err := Resolve(root, session); err == nil {
		t.Fatal("ambiguous rollout accepted")
	}

	root = t.TempDir()
	archived := filepath.Join(root, "archived_sessions")
	os.MkdirAll(archived, 0700)
	os.WriteFile(filepath.Join(archived, "rollout-2026-10-01T00-00-00-"+session+".jsonl"), []byte(meta(session)), 0600)
	if _, err := Resolve(root, session); err == nil {
		t.Fatal("archived rollout accepted")
	}

	for _, id := range []string{"", "../../secret", "*", "01A0AAAA-0000-7000-8000-000000000001", session + "x"} {
		if _, err := Resolve(root, id); err == nil {
			t.Fatalf("invalid ID %q accepted", id)
		}
	}
}

func TestResolveAcceptsLargeSessionMeta(t *testing.T) {
	root := t.TempDir()
	big := make([]byte, 256<<10)
	for i := range big {
		big[i] = 'x'
	}
	first := `{"timestamp":"t","type":"session_meta","ordinal":0,"payload":{"id":"` + session + `","base_instructions":{"text":"` + string(big) + `"}}}` + "\n"
	want := writeRollout(t, root, "2026/10/01", "2026-10-01T00-00-00", session, first)
	if path, err := Resolve(root, session); err != nil || path != want {
		t.Fatal(path, err)
	}
}
