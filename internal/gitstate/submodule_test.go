package gitstate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func head(t *testing.T, dir string) string {
	t.Helper()
	return strings.TrimSpace(git(t, dir, "rev-parse", "HEAD"))
}

// upstream builds the repository a test adds as a submodule; lib.txt
// differs between its two commits.
func upstream(t *testing.T, files map[string]string) (dir, first, second string) {
	t.Helper()
	dir = newRepo(t)
	for rel, content := range files {
		write(t, dir, rel, content)
	}
	write(t, dir, "lib.txt", "one\n")
	commitAll(t, dir)
	first = head(t, dir)
	write(t, dir, "lib.txt", "one\ntwo\n")
	commitAll(t, dir)
	return dir, first, head(t, dir)
}

// superproject is a repo with one commit and up added at sub, uncommitted.
// git 2.38.1 stopped cloning a local path as a submodule unless
// protocol.file.allow says so.
func superproject(t *testing.T, up string) (dir, sub string) {
	t.Helper()
	dir = newRepo(t)
	write(t, dir, "top.txt", "top\n")
	commitAll(t, dir)
	git(t, dir, "-c", "protocol.file.allow=always", "submodule", "add", "-q", up, "sub")
	return dir, filepath.Join(dir, "sub")
}

func nestedRepo(t *testing.T, dir, rel string, commit bool) string {
	t.Helper()
	nested := filepath.Join(dir, rel)
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, nested, "init", "-q", "-b", "main")
	write(t, nested, "inner.txt", "inner\n")
	if commit {
		commitAll(t, nested)
	}
	return nested
}

func onlyFile(t *testing.T, changes Changes, err error) File {
	t.Helper()
	if err != nil || changes.Total != 1 || len(changes.Files) != 1 {
		t.Fatalf("changes = %+v, %v", changes, err)
	}
	return changes.Files[0]
}

func TestSubmoduleCommitChangeIsAModifiedTextDiff(t *testing.T) {
	isolate(t)
	r := reader(t)
	ctx := context.Background()
	up, first, second := upstream(t, nil)
	dir, sub := superproject(t, up)
	git(t, sub, "checkout", "-q", first)
	commitAll(t, dir)

	moved := "@@ -1 +1 @@\n-Subproject commit " + first + "\n+Subproject commit " + second + "\n"
	check := func(stage string) {
		t.Helper()
		changes, err := r.Changes(ctx, dir)
		f := onlyFile(t, changes, err)
		if f.Path != "sub" || f.Status != StatusModified || f.Untracked || f.Binary || counts(f) != "+1-1" {
			t.Fatalf("%s: sub = %+v %s", stage, f, counts(f))
		}
		diff, err := r.Diff(ctx, dir, "sub")
		if err != nil || diff.Content != ContentText || diff.Truncated || !strings.Contains(diff.Text, " 160000\n") || !strings.HasSuffix(diff.Text, moved) {
			t.Fatalf("%s: diff = %+v, %v", stage, diff, err)
		}
	}
	git(t, sub, "checkout", "-q", second)
	check("work tree")
	git(t, dir, "add", "sub")
	check("index")
	write(t, sub, "lib.txt", "dirty\n")
	write(t, sub, "scratch.txt", "untracked\n")
	check("index with a dirty submodule")

	git(t, dir, "reset", "-q")
	if err := os.RemoveAll(sub); err != nil {
		t.Fatal(err)
	}
	changes, err := r.Changes(ctx, dir)
	if f := onlyFile(t, changes, err); f.Path != "sub" || f.Status != StatusDeleted || counts(f) != "+0-1" {
		t.Fatalf("removed sub = %+v %s", f, counts(f))
	}
	diff, err := r.Diff(ctx, dir, "sub")
	if err != nil || diff.Content != ContentText || !strings.Contains(diff.Text, "deleted file mode 160000\n") || !strings.HasSuffix(diff.Text, "@@ -1 +0,0 @@\n-Subproject commit "+first+"\n") {
		t.Fatalf("removed sub diff = %+v, %v", diff, err)
	}
}

func TestDirtySubmoduleWorkTreeIsNotAChange(t *testing.T) {
	isolate(t)
	r := reader(t)
	ctx := context.Background()
	up, _, _ := upstream(t, nil)
	dir, sub := superproject(t, up)
	commitAll(t, dir)
	write(t, sub, "lib.txt", "dirty\n")
	write(t, sub, "scratch.txt", "untracked\n")
	if got := git(t, dir, "status", "--porcelain=v2"); !strings.Contains(got, " S.MU ") {
		t.Fatalf("positive control: plain git status does not see the dirty submodule: %q", got)
	}

	changes, err := r.Changes(ctx, dir)
	if err != nil || !changes.Repository || changes.Total != 0 || len(changes.Files) != 0 {
		t.Fatalf("changes = %+v, %v", changes, err)
	}
	for _, path := range []string{"sub", "sub/lib.txt", "sub/scratch.txt"} {
		if _, err := r.Diff(ctx, dir, path); !errors.Is(err, ErrNotChanged) {
			t.Errorf("%s: %v", path, err)
		}
	}
}

func TestUncommittedSubmoduleAddListsGitmodulesAndGitlink(t *testing.T) {
	isolate(t)
	r := reader(t)
	ctx := context.Background()
	up, _, second := upstream(t, nil)
	dir, _ := superproject(t, up)

	changes, err := r.Changes(ctx, dir)
	if err != nil || changes.Total != 2 {
		t.Fatalf("changes = %+v, %v", changes, err)
	}
	files := byPath(t, changes)
	for path, want := range map[string]string{".gitmodules": "added +3-0", "sub": "added +1-0"} {
		f, ok := files[path]
		if got := f.Status + " " + counts(f); !ok || got != want || f.Untracked || f.Binary {
			t.Errorf("%s = %q %+v, want %q", path, got, f, want)
		}
	}
	diff, err := r.Diff(ctx, dir, "sub")
	if err != nil || diff.Content != ContentText || !strings.Contains(diff.Text, "new file mode 160000\n") || !strings.HasSuffix(diff.Text, "@@ -0,0 +1 @@\n+Subproject commit "+second+"\n") {
		t.Fatalf("sub diff = %+v, %v", diff, err)
	}
	diff, err = r.Diff(ctx, dir, ".gitmodules")
	if err != nil || diff.Content != ContentText || !strings.Contains(diff.Text, "+[submodule \"sub\"]\n+\tpath = sub\n+\turl = ") {
		t.Fatalf(".gitmodules diff = %+v, %v", diff, err)
	}
}

// TestUntrackedNestedRepositoryIsOneUnreadEntry covers a directory with its
// own .git that the superproject never added: git lists the directory itself,
// with a trailing slash, and the Reader neither counts nor reads inside it.
func TestUntrackedNestedRepositoryIsOneUnreadEntry(t *testing.T) {
	isolate(t)
	r := reader(t)
	ctx := context.Background()
	dir := newRepo(t)
	write(t, dir, "top.txt", "top\n")
	commitAll(t, dir)
	nested := nestedRepo(t, dir, "nested", true)
	write(t, nested, "inner.txt", "changed\n")
	write(t, nested, "loose.txt", "loose\n")
	nestedRepo(t, dir, "unborn", false)

	changes, err := r.Changes(ctx, dir)
	if err != nil || changes.Total != 2 {
		t.Fatalf("changes = %+v, %v", changes, err)
	}
	files := byPath(t, changes)
	for _, path := range []string{"nested/", "unborn/"} {
		f, ok := files[path]
		if !ok || f.Status != StatusAdded || !f.Untracked || f.Binary || counts(f) != "none" {
			t.Fatalf("%s = %+v", path, f)
		}
		diff, err := r.Diff(ctx, dir, path)
		if err != nil || diff.Content != ContentNone || diff.Text != "" || counts(diff.File) != "none" {
			t.Fatalf("%s diff = %+v, %v", path, diff, err)
		}
	}
	for _, path := range []string{"nested", "nested/inner.txt", "nested/loose.txt", "unborn/inner.txt"} {
		if _, err := r.Diff(ctx, dir, path); !errors.Is(err, ErrNotChanged) {
			t.Errorf("%s: %v", path, err)
		}
	}
}

// TestSubmoduleAndNestedRepoConfigRunsNothing arms a submodule and an
// untracked nested repository with their own command vectors. The filter
// driver name appears only in the submodule's config, so hardenedEnv cannot
// blank it: what keeps it from running is that --ignore-submodules=dirty
// compares the gitlink with the submodule HEAD without starting a git inside
// the submodule, and that diff-index ignores diff.submodule. In the positive
// control, plain git in the superproject reaches the submodule vectors through
// such child processes, and git run inside the nested repository proves its
// vectors are armed.
func TestSubmoduleAndNestedRepoConfigRunsNothing(t *testing.T) {
	isolate(t)
	r := reader(t)
	ctx := context.Background()
	marks := t.TempDir()
	script := func(name, tail string) string {
		path := filepath.Join(marks, name+".sh")
		body := fmt.Sprintf("#!/bin/sh\necho ran >> %q\n%s\n", filepath.Join(marks, name), tail)
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
		return path
	}
	hook := func(gitDir, name string) {
		path := filepath.Join(gitDir, "hooks", "post-index-change")
		if err := os.WriteFile(path, []byte(fmt.Sprintf("#!/bin/sh\necho ran >> %q\n", filepath.Join(marks, name))), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	vectors := []string{"sub-fsmonitor", "sub-external", "sub-diffdriver", "sub-filter", "sub-hook", "nested-fsmonitor", "nested-hook"}
	ran := func() []string {
		var out []string
		for _, name := range vectors {
			if _, err := os.Stat(filepath.Join(marks, name)); err == nil {
				out = append(out, name)
			}
		}
		return out
	}

	up, first, second := upstream(t, map[string]string{
		".gitattributes": "*.txt filter=subevil diff=subevil\n",
		"same.txt":       "same\n",
		"plain.md":       "plain\n",
	})
	dir, sub := superproject(t, up)
	git(t, sub, "checkout", "-q", first)
	commitAll(t, dir)
	git(t, sub, "checkout", "-q", second)
	nested := nestedRepo(t, dir, "nested", true)

	subGit := strings.TrimSpace(git(t, sub, "rev-parse", "--absolute-git-dir"))
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(subGit, filepath.Join(realDir, ".git", "modules")+string(filepath.Separator)) {
		t.Fatalf("submodule git dir %q is not under the superproject's .git/modules", subGit)
	}
	git(t, sub, "config", "core.fsmonitor", script("sub-fsmonitor", "exit 1"))
	git(t, sub, "config", "diff.external", script("sub-external", "exit 0"))
	git(t, sub, "config", "diff.subevil.command", script("sub-diffdriver", "exit 0"))
	filter := script("sub-filter", "cat")
	git(t, sub, "config", "filter.subevil.clean", filter)
	git(t, sub, "config", "filter.subevil.smudge", filter)
	git(t, sub, "config", "filter.subevil.required", "true")
	hook(subGit, "sub-hook")
	git(t, nested, "config", "core.fsmonitor", script("nested-fsmonitor", "exit 1"))
	hook(filepath.Join(nested, ".git"), "nested-hook")
	git(t, dir, "config", "diff.submodule", "diff")

	offset := 0
	stale := func() {
		offset += 3
		write(t, sub, "lib.txt", "one\ntwo\nthree\n")
		write(t, sub, "plain.md", "plain\nmore\n")
		write(t, nested, "inner.txt", "inner\nchanged\n")
		later := time.Now().Add(time.Duration(offset) * time.Second)
		for _, path := range []string{filepath.Join(sub, "same.txt"), filepath.Join(nested, "inner.txt")} {
			if err := os.Chtimes(path, later, later); err != nil {
				t.Fatal(err)
			}
		}
	}

	stale()
	git(t, dir, "status")
	git(t, dir, "diff", "HEAD")
	git(t, nested, "status")
	if got := strings.Join(ran(), ","); got != strings.Join(vectors, ",") {
		t.Fatalf("positive control ran %q; the repos no longer exercise every vector", got)
	}
	for _, name := range vectors {
		os.Remove(filepath.Join(marks, name))
	}

	stale()
	indexes := []string{filepath.Join(dir, ".git", "index"), filepath.Join(subGit, "index"), filepath.Join(nested, ".git", "index")}
	type stamp struct {
		data []byte
		mod  time.Time
	}
	read := func() []stamp {
		var out []stamp
		for _, path := range indexes {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, stamp{data, info.ModTime()})
		}
		return out
	}
	before := read()

	changes, err := r.Changes(ctx, dir)
	if err != nil || changes.Total != 2 {
		t.Errorf("changes = %+v, %v", changes, err)
	}
	files := byPath(t, changes)
	if f := files["sub"]; f.Status != StatusModified || counts(f) != "+1-1" {
		t.Errorf("sub = %+v", f)
	}
	if f := files["nested/"]; !f.Untracked || counts(f) != "none" {
		t.Errorf("nested/ = %+v", f)
	}
	diff, err := r.Diff(ctx, dir, "sub")
	if err != nil || diff.Content != ContentText || !strings.HasSuffix(diff.Text, "-Subproject commit "+first+"\n+Subproject commit "+second+"\n") {
		t.Errorf("sub diff = %+v, %v", diff, err)
	}
	if diff, err := r.Diff(ctx, dir, "nested/"); err != nil || diff.Content != ContentNone {
		t.Errorf("nested/ diff = %+v, %v", diff, err)
	}

	if got := ran(); len(got) != 0 {
		t.Errorf("Reader ran submodule or nested repo commands: %v", got)
	}
	for i, after := range read() {
		if !bytes.Equal(before[i].data, after.data) || !before[i].mod.Equal(after.mod) {
			t.Errorf("Reader rewrote %s", indexes[i])
		}
	}
}
