// Package gitstate reads a session project's Git working tree for the Changed
// Files view (ADR-016). It only reads: every git run is hardened against repo
// config that would execute a command or write the index, and untracked files
// are read without following symlinks.
package gitstate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	// MaxFiles caps the files one listing returns; Changes.Truncated marks the rest.
	MaxFiles = 500
	// MaxDiffBytes caps one file's unified diff; Diff.Truncated marks a cut.
	MaxDiffBytes = 256 << 10
	// maxListBytes caps the stdout of status and numstat. A listing past it
	// keeps the complete records it has and is marked truncated.
	maxListBytes = 8 << 20
	// maxCountBytes is the largest untracked file whose lines are counted, and
	// countBudget bounds what one listing reads for all of them.
	maxCountBytes = 4 << 20
	countBudget   = 32 << 20
	// sniffBytes is the prefix git itself inspects for NUL to call a file binary.
	sniffBytes     = 8000
	commandTimeout = 10 * time.Second
	waitDelay      = time.Second
	// concurrentReads bounds the git processes several open clients can start.
	concurrentReads = 2
)

var (
	ErrUnavailable    = errors.New("git executable unavailable")
	ErrProjectMissing = errors.New("project directory unavailable")
	ErrNotRepository  = errors.New("not a git work tree")
	ErrNotChanged     = errors.New("path is not in the current changes")
)

const (
	StatusModified = "modified"
	StatusAdded    = "added"
	StatusDeleted  = "deleted"
	StatusRenamed  = "renamed"
)

// File is one changed path relative to the repository root, compared with
// HEAD (index and working tree combined). Counts are nil when unknown: binary
// content, a non-regular untracked entry, or a listing cut short.
type File struct {
	Path      string `json:"path"`
	OldPath   string `json:"old_path,omitempty"`
	Status    string `json:"status"`
	Untracked bool   `json:"untracked,omitempty"`
	Binary    bool   `json:"binary,omitempty"`
	Additions *int   `json:"additions"`
	Deletions *int   `json:"deletions"`
}

type Changes struct {
	Repository bool `json:"repository"`
	// Branch is empty on a detached HEAD; Initial marks a branch without commits.
	Branch    string `json:"branch,omitempty"`
	Initial   bool   `json:"initial,omitempty"`
	Files     []File `json:"files"`
	Total     int    `json:"total"`
	Truncated bool   `json:"truncated"`
}

// Content says what Diff.Text holds: a unified diff (text), nothing because
// the file is binary (binary), or nothing because the entry is not a regular
// file the Bridge reads, such as an untracked symlink or nested repository (none).
const (
	ContentText   = "text"
	ContentBinary = "binary"
	ContentNone   = "none"
)

type Diff struct {
	File
	Content   string `json:"content"`
	Text      string `json:"diff"`
	Truncated bool   `json:"truncated"`
}

type Reader struct {
	git   string
	slots chan struct{}
}

// New uses the git executable at bin; an empty bin makes every read fail
// with ErrUnavailable instead of stopping the Bridge.
func New(bin string) *Reader {
	return &Reader{git: bin, slots: make(chan struct{}, concurrentReads)}
}

// Find resolves git once at startup so a later PATH change cannot swap it.
// An older git silently ignores GIT_CONFIG_COUNT, which would drop every
// override that keeps repo config from running a command, so it is refused.
func Find() (string, error) {
	path, err := exec.LookPath("git")
	if err != nil {
		return "", err
	}
	if path, err = filepath.Abs(path); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	out, _, err := runner{bin: path, env: baseEnv(nil)}.run(ctx, 4<<10, "--version")
	if err != nil {
		return "", err
	}
	if !supportedVersion(string(out)) {
		return "", fmt.Errorf("%s: %q is older than git %d.%d", path, strings.TrimSpace(string(out)), minMajor, minMinor)
	}
	return path, nil
}

// GIT_CONFIG_COUNT arrived in git 2.31.
const minMajor, minMinor = 2, 31

func supportedVersion(output string) bool {
	rest, ok := strings.CutPrefix(strings.TrimSpace(output), "git version ")
	if !ok {
		return false
	}
	parts := strings.SplitN(strings.Fields(rest + " ")[0], ".", 3)
	if len(parts) < 2 {
		return false
	}
	major, errMajor := strconv.Atoi(parts[0])
	minor, errMinor := strconv.Atoi(parts[1])
	if errMajor != nil || errMinor != nil {
		return false
	}
	return major > minMajor || major == minMajor && minor >= minMinor
}

func (r *Reader) acquire(ctx context.Context) (func(), error) {
	if r.git == "" {
		return nil, ErrUnavailable
	}
	select {
	case r.slots <- struct{}{}:
		return func() { <-r.slots }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Changes lists the working tree changes of the repository that contains dir.
// A dir outside any work tree is a Changes with Repository false, not an error.
func (r *Reader) Changes(ctx context.Context, dir string) (Changes, error) {
	release, err := r.acquire(ctx)
	if err != nil {
		return Changes{}, err
	}
	defer release()
	snap, err := r.snapshot(ctx, dir)
	if errors.Is(err, ErrNotRepository) {
		return Changes{Files: []File{}}, nil
	}
	if err != nil {
		return Changes{}, err
	}
	defer snap.root.Close()
	out := Changes{Repository: true, Branch: snap.branch, Initial: snap.initial, Total: len(snap.files), Truncated: snap.truncated}
	files := snap.files
	if len(files) > MaxFiles {
		files, out.Truncated = files[:MaxFiles], true
	}
	budget := int64(countBudget)
	for i := range files {
		if files[i].Untracked {
			countUntracked(snap.root, &files[i], &budget)
		}
	}
	out.Files = files
	return out, nil
}

// Diff returns the unified diff of one path from the current listing. A path
// that the listing does not contain exactly is ErrNotChanged; the caller's
// string never reaches git or the filesystem otherwise.
func (r *Reader) Diff(ctx context.Context, dir, path string) (Diff, error) {
	release, err := r.acquire(ctx)
	if err != nil {
		return Diff{}, err
	}
	defer release()
	snap, err := r.snapshot(ctx, dir)
	if err != nil {
		return Diff{}, err
	}
	defer snap.root.Close()
	var file *File
	for i := range snap.files {
		if snap.files[i].Path == path {
			file = &snap.files[i]
			break
		}
	}
	if file == nil {
		return Diff{}, ErrNotChanged
	}
	if file.Untracked {
		budget := int64(maxCountBytes)
		countUntracked(snap.root, file, &budget)
		return untrackedDiff(snap.root, *file), nil
	}
	if file.Binary {
		return Diff{File: *file, Content: ContentBinary}, nil
	}
	args := append(diffArgs(), "-p", snap.base, "--", file.Path)
	if file.OldPath != "" {
		args = append(args, file.OldPath)
	}
	text, cut, err := snap.git.run(ctx, MaxDiffBytes, args...)
	if err != nil {
		return Diff{}, err
	}
	if cut {
		text = cutAtLine(text)
	}
	return Diff{File: *file, Content: ContentText, Text: string(text), Truncated: cut}, nil
}

type snapshot struct {
	git       runner
	root      *os.Root
	base      string
	branch    string
	initial   bool
	files     []File
	truncated bool
}

func (r *Reader) snapshot(ctx context.Context, dir string) (snapshot, error) {
	if !filepath.IsAbs(dir) {
		return snapshot{}, ErrProjectMissing
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return snapshot{}, ErrProjectMissing
	}
	top, err := r.toplevel(ctx, dir)
	if err != nil {
		return snapshot{}, err
	}
	git := runner{bin: r.git, dir: top}
	if git.env, err = r.hardenedEnv(ctx, top); err != nil {
		return snapshot{}, err
	}
	status, statusCut, err := git.run(ctx, maxListBytes, "status", "--porcelain=v2", "-z", "--branch", "--no-ahead-behind", "--untracked-files=all", "--ignore-submodules=dirty", "--renames")
	if err != nil {
		return snapshot{}, err
	}
	parsed := parseStatus(status)
	snap := snapshot{git: git, branch: parsed.branch, initial: parsed.initial, files: parsed.files, truncated: statusCut}
	snap.base = parsed.oid
	if parsed.initial {
		tree, _, err := git.run(ctx, 128, "hash-object", "-t", "tree", "--no-filters", "--stdin")
		if err != nil {
			return snapshot{}, err
		}
		snap.base = strings.TrimSpace(string(tree))
	}
	if snap.base == "" {
		return snapshot{}, errors.New("git status reported no HEAD")
	}
	numstat, _, err := git.run(ctx, maxListBytes, append(diffArgs(), "--numstat", "-z", snap.base)...)
	if err != nil {
		return snapshot{}, err
	}
	applyNumstat(snap.files, parseNumstat(numstat))
	if snap.root, err = os.OpenRoot(top); err != nil {
		return snapshot{}, ErrProjectMissing
	}
	return snap, nil
}

// diffArgs is the shared diff invocation. It is the plumbing diff-index:
// porcelain `git diff <commit>` refreshes and rewrites .git/index even with
// --no-optional-locks. --no-ext-diff and --no-textconv keep diff.external and
// attribute drivers from running; --no-color and -M keep repo config from
// changing the output the client reads.
func diffArgs() []string {
	return []string{"diff-index", "-M", "--no-ext-diff", "--no-textconv", "--no-color", "--ignore-submodules=dirty"}
}

func (r *Reader) toplevel(ctx context.Context, dir string) (string, error) {
	git := runner{bin: r.git, dir: dir, env: baseEnv(nil)}
	out, _, err := git.run(ctx, 64<<10, "rev-parse", "--show-toplevel")
	if err != nil {
		var failed *gitError
		if errors.As(err, &failed) && (strings.Contains(failed.stderr, "not a git repository") || strings.Contains(failed.stderr, "must be run in a work tree")) {
			return "", ErrNotRepository
		}
		return "", err
	}
	top := strings.TrimSuffix(string(out), "\n")
	if top == "" || !filepath.IsAbs(top) {
		return "", ErrNotRepository
	}
	return top, nil
}

// hardenedEnv blanks every filter driver the repo's config defines. A stale
// stat makes status and diff hash the working tree file through its clean
// filter, which is a command from config, and git has no flag to skip filters
// for those commands. Reading config itself runs nothing.
func (r *Reader) hardenedEnv(ctx context.Context, top string) ([]string, error) {
	git := runner{bin: r.git, dir: top, env: baseEnv(nil)}
	out, _, err := git.run(ctx, 1<<20, "config", "--name-only", "-z", "--get-regexp", `^filter\.`)
	var failed *gitError
	if err != nil && !(errors.As(err, &failed) && failed.code == 1) {
		return nil, err
	}
	var drivers []string
	seen := map[string]bool{}
	for _, name := range strings.Split(string(out), "\x00") {
		dot := strings.LastIndexByte(name, '.')
		if !strings.HasPrefix(name, "filter.") || dot <= len("filter.") {
			continue
		}
		driver := name[len("filter."):dot]
		if !seen[driver] {
			seen[driver] = true
			drivers = append(drivers, driver)
		}
	}
	var overrides [][2]string
	for _, driver := range drivers {
		for _, key := range []string{"clean", "smudge", "process"} {
			overrides = append(overrides, [2]string{"filter." + driver + "." + key, ""})
		}
		overrides = append(overrides, [2]string{"filter." + driver + ".required", "false"})
	}
	return baseEnv(overrides), nil
}

// baseEnv is the whole environment of a git run. It drops whatever GIT_DIR,
// GIT_INDEX_FILE or GIT_CONFIG_PARAMETERS the Bridge inherited. Overrides go
// through GIT_CONFIG_COUNT rather than -c so a driver name containing '='
// cannot be misparsed, and command scope wins over every config file.
// System and global config stay: they hold excludesFile and the like, and the
// overrides neutralize the exec vectors whichever file sets them.
func baseEnv(extra [][2]string) []string {
	env := []string{"LC_ALL=C", "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "GIT_PAGER=cat", "GIT_LITERAL_PATHSPECS=1"}
	for _, name := range []string{"PATH", "HOME", "XDG_CONFIG_HOME"} {
		if value, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+value)
		}
	}
	// core.fsmonitor names a hook command run by status and diff; hooksPath
	// guards hooks such as post-index-change should an index write slip through.
	overrides := append([][2]string{{"core.fsmonitor", "false"}, {"core.hooksPath", os.DevNull}}, extra...)
	env = append(env, "GIT_CONFIG_COUNT="+strconv.Itoa(len(overrides)))
	for i, kv := range overrides {
		env = append(env, fmt.Sprintf("GIT_CONFIG_KEY_%d=%s", i, kv[0]), fmt.Sprintf("GIT_CONFIG_VALUE_%d=%s", i, kv[1]))
	}
	return env
}

type runner struct {
	bin string
	dir string
	env []string
}

type gitError struct {
	args   []string
	code   int
	stderr string
	err    error
}

func (e *gitError) Error() string {
	return fmt.Sprintf("git %s: %v: %s", strings.Join(e.args, " "), e.err, e.stderr)
}
func (e *gitError) Unwrap() error { return e.err }

// run executes git with a fixed argument list and no shell. Output past limit
// is discarded so the child never blocks; cut reports that it happened.
func (g runner) run(ctx context.Context, limit int, args ...string) (out []byte, cut bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	full := append([]string{"--no-pager", "--no-optional-locks", "--literal-pathspecs"}, args...)
	cmd := exec.CommandContext(ctx, g.bin, full...)
	cmd.Dir = g.dir
	cmd.Env = g.env
	cmd.WaitDelay = waitDelay
	stdout := &capped{limit: limit}
	stderr := &capped{limit: 16 << 10}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err = cmd.Run(); err != nil {
		code := -1
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			code = exit.ExitCode()
		}
		return nil, false, &gitError{args: args, code: code, stderr: strings.TrimSpace(stderr.buf.String()), err: err}
	}
	return stdout.buf.Bytes(), stdout.exceeded, nil
}

// capped keeps at most limit bytes. It does not embed bytes.Buffer: a
// promoted ReadFrom would let io.Copy bypass Write.
type capped struct {
	buf      bytes.Buffer
	limit    int
	exceeded bool
}

func (b *capped) Write(p []byte) (int, error) {
	if room := b.limit - b.buf.Len(); len(p) > room {
		b.exceeded = true
		if room > 0 {
			b.buf.Write(p[:room])
		}
		return len(p), nil
	}
	return b.buf.Write(p)
}

func cutAtLine(text []byte) []byte {
	if i := bytes.LastIndexByte(text, '\n'); i >= 0 {
		return text[:i+1]
	}
	return text[:0]
}

type parsedStatus struct {
	branch  string
	oid     string
	initial bool
	files   []File
}

// parseStatus reads `git status --porcelain=v2 -z --branch`. Only records
// ending in NUL are used, so a listing cut at maxListBytes loses its partial
// last record instead of yielding a truncated path.
func parseStatus(out []byte) parsedStatus {
	var parsed parsedStatus
	fields := strings.Split(string(out), "\x00")
	fields = fields[:len(fields)-1]
	seen := map[string]bool{}
	add := func(file File) {
		if seen[file.Path] {
			return
		}
		seen[file.Path] = true
		parsed.files = append(parsed.files, file)
	}
	for i := 0; i < len(fields); i++ {
		record := fields[i]
		if record == "" {
			continue
		}
		switch record[0] {
		case '#':
			if value, ok := strings.CutPrefix(record, "# branch.oid "); ok {
				if value == "(initial)" {
					parsed.initial = true
				} else {
					parsed.oid = value
				}
			} else if value, ok := strings.CutPrefix(record, "# branch.head "); ok && value != "(detached)" {
				parsed.branch = value
			}
		case '1':
			parts := strings.SplitN(record, " ", 9)
			if len(parts) != 9 || len(parts[1]) != 2 {
				continue
			}
			x, y := parts[1][0], parts[1][1]
			switch {
			case x == 'A' && y == 'D':
			case x == 'A':
				add(File{Path: parts[8], Status: StatusAdded})
			case x == 'D' || y == 'D':
				add(File{Path: parts[8], Status: StatusDeleted})
			default:
				add(File{Path: parts[8], Status: StatusModified})
			}
		case '2':
			if i+1 >= len(fields) {
				continue
			}
			original := fields[i+1]
			i++
			parts := strings.SplitN(record, " ", 10)
			if len(parts) != 10 || len(parts[1]) != 2 {
				continue
			}
			x, y := parts[1][0], parts[1][1]
			switch {
			case x == 'R' && y == 'D':
				add(File{Path: original, Status: StatusDeleted})
			case x == 'R':
				add(File{Path: parts[9], OldPath: original, Status: StatusRenamed})
			case y != 'D':
				add(File{Path: parts[9], Status: StatusAdded})
			}
		case 'u':
			parts := strings.SplitN(record, " ", 11)
			if len(parts) == 11 {
				add(File{Path: parts[10], Status: StatusModified})
			}
		case '?':
			if len(record) > 2 {
				add(File{Path: record[2:], Status: StatusAdded, Untracked: true})
			}
		}
	}
	if parsed.files == nil {
		parsed.files = []File{}
	}
	return parsed
}

type count struct {
	additions, deletions int
	binary               bool
}

// parseNumstat reads `git diff --numstat -z`. A rename record has an empty
// path field followed by the old and new paths; counts are keyed by new path.
func parseNumstat(out []byte) map[string]count {
	counts := map[string]count{}
	fields := strings.Split(string(out), "\x00")
	fields = fields[:len(fields)-1]
	for i := 0; i < len(fields); i++ {
		parts := strings.SplitN(fields[i], "\t", 3)
		if len(parts) != 3 {
			continue
		}
		path := parts[2]
		if path == "" {
			if i+2 >= len(fields) {
				break
			}
			path = fields[i+2]
			i += 2
		}
		if parts[0] == "-" && parts[1] == "-" {
			counts[path] = count{binary: true}
			continue
		}
		added, errA := strconv.Atoi(parts[0])
		deleted, errD := strconv.Atoi(parts[1])
		if errA == nil && errD == nil {
			counts[path] = count{additions: added, deletions: deleted}
		}
	}
	return counts
}

func applyNumstat(files []File, counts map[string]count) {
	for i := range files {
		c, ok := counts[files[i].Path]
		if !ok || files[i].Untracked {
			continue
		}
		if c.binary {
			files[i].Binary = true
			continue
		}
		files[i].Additions, files[i].Deletions = intPtr(c.additions), intPtr(c.deletions)
	}
}

func intPtr(n int) *int { return &n }

// readUntracked reads a regular untracked file under root without following
// a symlink: the entry must be regular at Lstat, and the opened file must be
// that same file, so a swap to a symlink or FIFO after Lstat is refused.
// O_NONBLOCK keeps a raced-in FIFO from blocking the open.
func readUntracked(root *os.Root, rel string, limit int64) (data []byte, cut bool, ok bool) {
	info, err := root.Lstat(rel)
	if err != nil || !info.Mode().IsRegular() {
		return nil, false, false
	}
	f, err := root.OpenFile(rel, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, false, false
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) || !opened.Mode().IsRegular() {
		return nil, false, false
	}
	data, err = io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, false, false
	}
	if int64(len(data)) > limit {
		return data[:limit], true, true
	}
	return data, false, true
}

func isBinary(data []byte) bool {
	return bytes.IndexByte(data[:min(len(data), sniffBytes)], 0) >= 0
}

func lineCount(data []byte) int {
	n := bytes.Count(data, []byte{'\n'})
	if len(data) > 0 && data[len(data)-1] != '\n' {
		n++
	}
	return n
}

func countUntracked(root *os.Root, file *File, budget *int64) {
	info, err := root.Lstat(file.Path)
	if err != nil || !info.Mode().IsRegular() {
		return
	}
	if info.Size() > maxCountBytes || info.Size() > *budget {
		return
	}
	data, cut, ok := readUntracked(root, file.Path, maxCountBytes)
	if !ok || cut {
		return
	}
	*budget -= int64(len(data))
	if isBinary(data) {
		file.Binary = true
		return
	}
	file.Additions, file.Deletions = intPtr(lineCount(data)), intPtr(0)
}

// untrackedDiff renders a new file as a unified diff against nothing, the
// shape git prints for an added file, so the client reads one format.
func untrackedDiff(root *os.Root, file File) Diff {
	data, cut, ok := readUntracked(root, file.Path, MaxDiffBytes)
	if !ok {
		return Diff{File: file, Content: ContentNone}
	}
	if isBinary(data) {
		file.Binary, file.Additions, file.Deletions = true, nil, nil
		return Diff{File: file, Content: ContentBinary}
	}
	if cut {
		data = cutAtLine(data)
	}
	if len(data) == 0 {
		return Diff{File: file, Content: ContentText, Truncated: cut}
	}
	lines := strings.SplitAfter(string(data), "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	var b strings.Builder
	fmt.Fprintf(&b, "--- /dev/null\n+++ b/%s\n@@ -0,0 +1,%d @@\n", file.Path, len(lines))
	for _, line := range lines {
		b.WriteByte('+')
		b.WriteString(line)
	}
	if !strings.HasSuffix(b.String(), "\n") {
		b.WriteString("\n\\ No newline at end of file\n")
	}
	return Diff{File: file, Content: ContentText, Text: b.String(), Truncated: cut}
}
