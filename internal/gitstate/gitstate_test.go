package gitstate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// isolate keeps the developer's global and system git config out of the
// repos these tests build; the Reader still sees HOME like the Bridge does.
func isolate(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

func reader(t *testing.T) *Reader {
	t.Helper()
	bin, err := Find()
	if err != nil {
		t.Skip("git not installed")
	}
	return New(bin)
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	return dir
}

func commitAll(t *testing.T, dir string) {
	t.Helper()
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "c")
}

func byPath(t *testing.T, changes Changes) map[string]File {
	t.Helper()
	out := map[string]File{}
	for _, f := range changes.Files {
		out[f.Path] = f
	}
	return out
}

func counts(f File) string {
	if f.Additions == nil || f.Deletions == nil {
		return "none"
	}
	return fmt.Sprintf("+%d-%d", *f.Additions, *f.Deletions)
}

func TestChangesCombineIndexAndWorkTreeAgainstHead(t *testing.T) {
	isolate(t)
	r := reader(t)
	dir := newRepo(t)
	write(t, dir, "a.txt", "one\ntwo\n")
	write(t, dir, "gone.txt", "x\ny\nz\n")
	write(t, dir, "old name.txt", strings.Repeat("same line\n", 20))
	write(t, dir, "image.bin", "\x00\x01\x02")
	commitAll(t, dir)

	write(t, dir, "a.txt", "one\nTWO\n")
	git(t, dir, "add", "a.txt")
	write(t, dir, "a.txt", "one\nTWO\nthree\n")
	if err := os.Remove(filepath.Join(dir, "gone.txt")); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "mv", "old name.txt", "new name.txt")
	write(t, dir, "image.bin", "\x00\x09")
	write(t, dir, "nested/dir/new.txt", "first\nsecond")
	write(t, dir, "staged.txt", "s\n")
	git(t, dir, "add", "staged.txt")

	changes, err := r.Changes(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if !changes.Repository || changes.Branch != "main" || changes.Initial || changes.Truncated || changes.Total != 6 {
		t.Fatalf("changes = %+v", changes)
	}
	files := byPath(t, changes)
	for path, want := range map[string]string{
		"a.txt":              "modified +2-1",
		"gone.txt":           "deleted +0-3",
		"new name.txt":       "renamed +0-0",
		"image.bin":          "modified none",
		"nested/dir/new.txt": "added +2-0",
		"staged.txt":         "added +1-0",
	} {
		f, ok := files[path]
		if got := f.Status + " " + counts(f); !ok || got != want {
			t.Errorf("%s = %q, want %q", path, got, want)
		}
	}
	if files["new name.txt"].OldPath != "old name.txt" || !files["image.bin"].Binary || !files["nested/dir/new.txt"].Untracked || files["staged.txt"].Untracked {
		t.Fatalf("files = %+v", files)
	}
}

func TestUnbornHeadComparesWithTheEmptyTree(t *testing.T) {
	isolate(t)
	r := reader(t)
	dir := newRepo(t)
	write(t, dir, "staged.txt", "a\nb\n")
	git(t, dir, "add", "staged.txt")
	write(t, dir, "loose.txt", "c\n")
	changes, err := r.Changes(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	files := byPath(t, changes)
	if !changes.Initial || changes.Branch != "main" || counts(files["staged.txt"]) != "+2-0" || files["staged.txt"].Status != StatusAdded || !files["loose.txt"].Untracked {
		t.Fatalf("changes = %+v", changes)
	}
	diff, err := r.Diff(context.Background(), dir, "staged.txt")
	if err != nil || !strings.Contains(diff.Text, "+a\n+b\n") {
		t.Fatalf("diff = %+v, %v", diff, err)
	}
}

func TestNonRepositoryAndMissingProject(t *testing.T) {
	isolate(t)
	r := reader(t)
	plain := t.TempDir()
	changes, err := r.Changes(context.Background(), plain)
	if err != nil || changes.Repository || changes.Files == nil {
		t.Fatalf("plain dir = %+v, %v", changes, err)
	}
	if _, err := r.Diff(context.Background(), plain, "a"); !errors.Is(err, ErrNotRepository) {
		t.Fatalf("diff in plain dir: %v", err)
	}
	for _, dir := range []string{filepath.Join(plain, "missing"), "relative/dir", ""} {
		if _, err := r.Changes(context.Background(), dir); !errors.Is(err, ErrProjectMissing) {
			t.Fatalf("%q: %v", dir, err)
		}
	}
	if _, err := New("").Changes(context.Background(), plain); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("no git: %v", err)
	}
}

func TestSubdirectoryAndLinkedWorktreeUseTheWorkTreeRoot(t *testing.T) {
	isolate(t)
	r := reader(t)
	dir := newRepo(t)
	write(t, dir, "pkg/a.txt", "a\n")
	commitAll(t, dir)
	write(t, dir, "pkg/a.txt", "b\n")
	changes, err := r.Changes(context.Background(), filepath.Join(dir, "pkg"))
	if err != nil || len(changes.Files) != 1 || changes.Files[0].Path != "pkg/a.txt" {
		t.Fatalf("subdir = %+v, %v", changes, err)
	}
	linked := filepath.Join(t.TempDir(), "wt")
	git(t, dir, "worktree", "add", "-q", "-b", "side", linked)
	write(t, linked, "pkg/a.txt", "c\nd\n")
	changes, err = r.Changes(context.Background(), linked)
	if err != nil || changes.Branch != "side" || len(changes.Files) != 1 || counts(changes.Files[0]) != "+2-1" {
		t.Fatalf("linked worktree = %+v, %v", changes, err)
	}
	diff, err := r.Diff(context.Background(), linked, "pkg/a.txt")
	if err != nil || !strings.Contains(diff.Text, "+c\n+d\n") || strings.Contains(diff.Text, "+b") {
		t.Fatalf("linked diff = %+v, %v", diff, err)
	}
}

func TestDiffShowsOnlyAListedPath(t *testing.T) {
	isolate(t)
	r := reader(t)
	dir := newRepo(t)
	write(t, dir, "a.txt", "old\n")
	write(t, dir, "*.txt", "glob name\n")
	write(t, dir, "clean.txt", "clean\n")
	write(t, dir, "from.txt", strings.Repeat("keep\n", 10))
	commitAll(t, dir)
	write(t, dir, "a.txt", "new\n")
	git(t, dir, "mv", "from.txt", "to.txt")
	write(t, dir, "to.txt", strings.Repeat("keep\n", 10)+"more\n")
	write(t, dir, "untracked.txt", "u1\nu2")

	diff, err := r.Diff(context.Background(), dir, "a.txt")
	if err != nil || diff.Content != ContentText || !strings.Contains(diff.Text, "-old\n+new\n") || diff.Status != StatusModified {
		t.Fatalf("a.txt = %+v, %v", diff, err)
	}
	diff, err = r.Diff(context.Background(), dir, "to.txt")
	if err != nil || diff.Status != StatusRenamed || !strings.Contains(diff.Text, "rename from from.txt") || !strings.Contains(diff.Text, "+more\n") {
		t.Fatalf("rename = %+v, %v", diff, err)
	}
	diff, err = r.Diff(context.Background(), dir, "untracked.txt")
	if err != nil || diff.Text != "--- /dev/null\n+++ b/untracked.txt\n@@ -0,0 +1,2 @@\n+u1\n+u2\n\\ No newline at end of file\n" || counts(diff.File) != "+2-0" {
		t.Fatalf("untracked = %+v, %v", diff, err)
	}
	for _, path := range []string{"", "./a.txt", "clean.txt", "../a.txt", "*.txt", ":(glob)*", "a.txt\x00", "A.TXT", filepath.Join(dir, "a.txt")} {
		if _, err := r.Diff(context.Background(), dir, path); !errors.Is(err, ErrNotChanged) {
			t.Errorf("%q: %v", path, err)
		}
	}
}

func TestLiteralPathspecKeepsAGlobNameToItself(t *testing.T) {
	isolate(t)
	r := reader(t)
	dir := newRepo(t)
	write(t, dir, "*.md", "star\n")
	write(t, dir, "secret.md", "hidden\n")
	commitAll(t, dir)
	write(t, dir, "*.md", "star changed\n")
	write(t, dir, "secret.md", "hidden changed\n")
	diff, err := r.Diff(context.Background(), dir, "*.md")
	if err != nil || !strings.Contains(diff.Text, "+star changed") || strings.Contains(diff.Text, "hidden") {
		t.Fatalf("diff = %+v, %v", diff, err)
	}
}

func TestUntrackedSymlinkAndFIFOAreNotRead(t *testing.T) {
	isolate(t)
	r := reader(t)
	dir := newRepo(t)
	write(t, dir, "tracked.txt", "t\n")
	commitAll(t, dir)
	secret := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(secret, []byte("outside secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(dir, "link.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("tracked.txt", filepath.Join(dir, "inner-link.txt")); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(dir, "pipe")
	if err := mkfifo(fifo); err != nil {
		t.Skip("mkfifo:", err)
	}
	changes, err := r.Changes(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	files := byPath(t, changes)
	for _, path := range []string{"link.txt", "inner-link.txt"} {
		f, ok := files[path]
		if !ok || f.Status != StatusAdded || counts(f) != "none" || f.Binary {
			t.Fatalf("%s = %+v", path, f)
		}
		done := make(chan struct{})
		var diff Diff
		go func() { diff, err = r.Diff(context.Background(), dir, path); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("diff blocked")
		}
		if err != nil || diff.Content != ContentNone || diff.Text != "" {
			t.Fatalf("%s diff = %+v, %v", path, diff, err)
		}
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if _, _, ok := readUntracked(root, "pipe", 10); ok {
		t.Fatal("FIFO read")
	}
	if _, _, ok := readUntracked(root, "../"+filepath.Base(secret), 10); ok {
		t.Fatal("escaped the root")
	}
}

func TestLargeListingAndDiffAreTruncated(t *testing.T) {
	isolate(t)
	r := reader(t)
	dir := newRepo(t)
	write(t, dir, "big.txt", "base\n")
	commitAll(t, dir)
	line := strings.Repeat("x", 99) + "\n"
	write(t, dir, "big.txt", strings.Repeat(line, MaxDiffBytes/len(line)+500))
	for i := range MaxFiles + 5 {
		write(t, dir, fmt.Sprintf("many/f%04d.txt", i), "n\n")
	}
	changes, err := r.Changes(context.Background(), dir)
	if err != nil || !changes.Truncated || len(changes.Files) != MaxFiles || changes.Total != MaxFiles+6 {
		t.Fatalf("listing: truncated=%v files=%d total=%d err=%v", changes.Truncated, len(changes.Files), changes.Total, err)
	}
	diff, err := r.Diff(context.Background(), dir, "big.txt")
	if err != nil || !diff.Truncated || len(diff.Text) > MaxDiffBytes || !strings.HasSuffix(diff.Text, "\n") || counts(diff.File) == "none" {
		t.Fatalf("big diff: truncated=%v len=%d err=%v", diff.Truncated, len(diff.Text), err)
	}
	last, err := r.Diff(context.Background(), dir, fmt.Sprintf("many/f%04d.txt", MaxFiles+4))
	if err != nil || last.Text == "" {
		t.Fatalf("a path past the listing cap is still a current change: %+v, %v", last, err)
	}
	write(t, dir, "big-new.txt", strings.Repeat(line, MaxDiffBytes/len(line)+500))
	diff, err = r.Diff(context.Background(), dir, "big-new.txt")
	if err != nil || !diff.Truncated || !strings.HasSuffix(diff.Text, "\n") || strings.Contains(diff.Text, "No newline") {
		t.Fatalf("big untracked: truncated=%v err=%v", diff.Truncated, err)
	}
}

func TestBinaryUntrackedFile(t *testing.T) {
	isolate(t)
	r := reader(t)
	dir := newRepo(t)
	write(t, dir, "blob.bin", "abc\x00def")
	changes, err := r.Changes(context.Background(), dir)
	if err != nil || len(changes.Files) != 1 || !changes.Files[0].Binary || counts(changes.Files[0]) != "none" {
		t.Fatalf("changes = %+v, %v", changes, err)
	}
	diff, err := r.Diff(context.Background(), dir, "blob.bin")
	if err != nil || diff.Content != ContentBinary || diff.Text != "" {
		t.Fatalf("diff = %+v, %v", diff, err)
	}
}

// TestRepoConfigRunsNothing builds a repo whose config would run a command
// from status or diff, proves plain git runs each one (the positive control),
// then proves the Reader runs none of them and leaves the index untouched.
func TestRepoConfigRunsNothing(t *testing.T) {
	isolate(t)
	r := reader(t)
	dir := newRepo(t)
	marks := t.TempDir()
	script := func(name, tail string) string {
		path := filepath.Join(marks, name+".sh")
		body := fmt.Sprintf("#!/bin/sh\necho ran >> %q\n%s\n", filepath.Join(marks, name), tail)
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
		return path
	}
	write(t, dir, ".gitattributes", "*.txt filter=evil diff=evil\n*.conv diff=conv\n")
	write(t, dir, "a.txt", "one\n")
	write(t, dir, "same.txt", "same\n")
	write(t, dir, "b.conv", "conv\n")
	commitAll(t, dir)
	git(t, dir, "config", "core.fsmonitor", script("fsmonitor", "exit 1"))
	git(t, dir, "config", "diff.external", script("external", "exit 0"))
	git(t, dir, "config", "diff.evil.command", script("diffdriver", "exit 0"))
	git(t, dir, "config", "diff.conv.textconv", script("textconv", `cat "$1"`))
	filter := script("filter", "cat")
	git(t, dir, "config", "filter.evil.clean", filter)
	git(t, dir, "config", "filter.evil.smudge", filter)
	git(t, dir, "config", "filter.evil.required", "true")
	git(t, dir, "config", "core.pager", script("pager", "cat"))
	hooks := filepath.Join(dir, ".git", "hooks")
	if err := os.WriteFile(filepath.Join(hooks, "post-index-change"), []byte(fmt.Sprintf("#!/bin/sh\necho ran >> %q\n", filepath.Join(marks, "hook"))), 0o755); err != nil {
		t.Fatal(err)
	}

	offset := 0
	stale := func() {
		offset += 3
		write(t, dir, "a.txt", "one\ntwo\n")
		write(t, dir, "b.conv", "conv\nmore\n")
		later := time.Now().Add(time.Duration(offset) * time.Second)
		if err := os.Chtimes(filepath.Join(dir, "same.txt"), later, later); err != nil {
			t.Fatal(err)
		}
	}
	ran := func() []string {
		var out []string
		for _, name := range []string{"fsmonitor", "external", "diffdriver", "textconv", "filter", "pager", "hook"} {
			if _, err := os.Stat(filepath.Join(marks, name)); err == nil {
				out = append(out, name)
			}
		}
		return out
	}

	stale()
	git(t, dir, "status")
	git(t, dir, "diff", "HEAD")
	git(t, dir, "diff", "HEAD", "--", "b.conv")
	git(t, dir, "diff", "--no-ext-diff", "HEAD", "--", "b.conv")
	if got := strings.Join(ran(), ","); got != "fsmonitor,external,diffdriver,textconv,filter,hook" {
		t.Fatalf("positive control ran %q; the repo config no longer exercises every vector", got)
	}
	for _, name := range []string{"fsmonitor", "external", "diffdriver", "textconv", "filter", "hook"} {
		os.Remove(filepath.Join(marks, name))
	}

	stale()
	index := filepath.Join(dir, ".git", "index")
	before, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	beforeInfo, _ := os.Stat(index)
	changes, err := r.Changes(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if f := byPath(t, changes)["a.txt"]; counts(f) != "+1-0" {
		t.Fatalf("a.txt = %+v", f)
	}
	for _, path := range []string{"a.txt", "b.conv"} {
		diff, err := r.Diff(context.Background(), dir, path)
		if err != nil || diff.Content != ContentText || !strings.Contains(diff.Text, "@@") {
			t.Fatalf("%s diff = %+v, %v", path, diff, err)
		}
	}
	if got := ran(); len(got) != 0 {
		t.Fatalf("Reader ran repo config commands: %v", got)
	}
	after, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	afterInfo, _ := os.Stat(index)
	if !bytes.Equal(before, after) || !beforeInfo.ModTime().Equal(afterInfo.ModTime()) {
		t.Fatal("Reader rewrote .git/index")
	}
}

// TestOversizedFilterConfigFailsClosed pads the config so the filter listing
// passes its cap before the armed driver, which the cut would otherwise leave
// out of the overrides.
func TestOversizedFilterConfigFailsClosed(t *testing.T) {
	isolate(t)
	r := reader(t)
	dir := newRepo(t)
	marks := t.TempDir()
	filter := filepath.Join(marks, "filter.sh")
	ran := filepath.Join(marks, "ran")
	if err := os.WriteFile(filter, []byte(fmt.Sprintf("#!/bin/sh\necho ran >> %q\ncat\n", ran)), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, dir, ".gitattributes", "*.txt filter=evil\n")
	write(t, dir, "same.txt", "same\n")
	commitAll(t, dir)

	entry := "\tclean = x\n"
	padding := strings.Repeat(entry, maxFilterConfigBytes/len("filter.a.clean\x00")+1)
	config, err := os.OpenFile(filepath.Join(dir, ".git", "config"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = fmt.Fprintf(config, "[filter \"a\"]\n%s[filter \"evil\"]\n\tclean = %q\n\trequired = true\n", padding, filter)
	if closeErr := config.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(5 * time.Second)
	if err := os.Chtimes(filepath.Join(dir, "same.txt"), later, later); err != nil {
		t.Fatal(err)
	}

	if _, err := r.Changes(context.Background(), dir); err == nil {
		t.Fatal("Changes read a repo whose filter config passed the cap")
	}
	if _, err := r.Diff(context.Background(), dir, "same.txt"); err == nil {
		t.Fatal("Diff read a repo whose filter config passed the cap")
	}
	if _, err := os.Stat(ran); err == nil {
		t.Fatal("Reader ran the filter left out of the overrides")
	}
	git(t, dir, "status")
	if _, err := os.Stat(ran); err != nil {
		t.Fatal("positive control: plain git status did not run the filter")
	}
}

func TestParseStatusDropsAPartialRecord(t *testing.T) {
	full := "# branch.oid abc\x00# branch.head main\x00" +
		"1 .M N... 100644 100644 100644 h1 h2 a b.txt\x00" +
		"2 R. N... 100644 100644 100644 h1 h2 R100 new.txt\x00old.txt\x00" +
		"? untracked.txt\x00"
	parsed := parseStatus([]byte(full))
	if parsed.oid != "abc" || parsed.branch != "main" || len(parsed.files) != 3 || parsed.files[0].Path != "a b.txt" || parsed.files[1].OldPath != "old.txt" {
		t.Fatalf("parsed = %+v", parsed)
	}
	cut := parseStatus([]byte(full[:len(full)-4]))
	if len(cut.files) != 2 {
		t.Fatalf("cut = %+v", cut.files)
	}
	renameCut := parseStatus([]byte("2 R. N... 100644 100644 100644 h1 h2 R100 new.txt\x00old"))
	if len(renameCut.files) != 0 {
		t.Fatalf("rename without its original path = %+v", renameCut.files)
	}
}

func TestGitOlderThanConfigEnvIsRefused(t *testing.T) {
	for output, want := range map[string]bool{
		"git version 2.54.0 (Apple Git-157)\n": true,
		"git version 2.31.0\n":                 true,
		"git version 3.0\n":                    true,
		"git version 2.30.9\n":                 false,
		"git version 1.99.0\n":                 false,
		"git version 2.windows.1\n":            false,
		"version 2.40.0\n":                     false,
		"":                                     false,
	} {
		if got := supportedVersion(output); got != want {
			t.Errorf("%q = %v, want %v", output, got, want)
		}
	}
}

func TestParseNumstatRenameAndBinary(t *testing.T) {
	got := parseNumstat([]byte("1\t2\ta.txt\x00-\t-\tbin\x003\t0\t\x00old.txt\x00new.txt\x00"))
	if got["a.txt"] != (count{1, 2, false}) || !got["bin"].binary || got["new.txt"] != (count{3, 0, false}) || len(got) != 3 {
		t.Fatalf("numstat = %+v", got)
	}
}
