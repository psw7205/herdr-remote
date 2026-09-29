package session

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/psw7205/herdr-remote/internal/herdr"
)

// Placement of a new session (ADR-037). An open workspace always gets a new
// tab so the same cwd never gains a second workspace.
const (
	PlacementNewWorkspace = "new_workspace"
	PlacementNewTab       = "new_tab"
)

// startKinds is the agent allowlist. A kind is added only after its
// transcript adapter and conditional input are verified.
var startKinds = []string{"claude"}

// maxCandidates bounds one listing of a large root.
const maxCandidates = 256

type StartGateway interface {
	Topology(context.Context) (herdr.Topology, error)
	WorkspaceCreate(context.Context, string) (herdr.Created, error)
	TabCreate(context.Context, string, string) (herdr.Created, error)
	StartAgent(context.Context, string, string, string) error
}

// Candidate is a folder a new session may start in. Clients see only the
// opaque ID and display names, never the path.
type Candidate struct {
	ID string `json:"id"`
	// Name is the folder's base name; Root is the base name of the
	// -project-root it was found under, empty for an open workspace outside
	// every root.
	Name string `json:"name"`
	Root string `json:"root,omitempty"`
	Open bool   `json:"open"`
	// Workspace is the Herdr label of the open workspace.
	Workspace string `json:"workspace,omitempty"`
	Placement string `json:"placement"`

	path        string
	root        string
	workspaceID string
}

// StartResult is the dispatch outcome recorded in the session_start receipt.
// Created is set as soon as Herdr made a workspace or tab, including when the
// agent then failed to start.
type StartResult struct {
	Status  string
	Code    string
	Created *herdr.Created
	Agent   string
}

type Starter struct {
	gateway StartGateway
	roots   []string
	// mu serializes starts so two command IDs cannot both see a folder as
	// closed and open two workspaces for it.
	mu sync.Mutex
}

// NewStarter resolves each root to its real directory. Roots are the trust
// boundary for new workspaces; without one only open workspaces get new tabs.
func NewStarter(gateway StartGateway, roots []string) (*Starter, error) {
	var real []string
	for _, root := range roots {
		if !filepath.IsAbs(root) {
			return nil, fmt.Errorf("project root must be absolute: %q", root)
		}
		resolved, err := filepath.EvalSymlinks(root)
		if err != nil {
			return nil, fmt.Errorf("project root unavailable: %w", err)
		}
		if info, err := os.Stat(resolved); err != nil || !info.IsDir() {
			return nil, fmt.Errorf("project root is not a directory: %q", root)
		}
		if !slices.Contains(real, resolved) {
			real = append(real, resolved)
		}
	}
	return &Starter{gateway: gateway, roots: real}, nil
}

func (s *Starter) Kinds() []string    { return slices.Clone(startKinds) }
func (s *Starter) NewWorkspace() bool { return len(s.roots) > 0 }

// candidateID is stable across Bridge restarts so a retried command resolves
// to the same folder, and reveals nothing about the path by itself.
func candidateID(path string) string {
	sum := sha256.Sum256([]byte("herdr-remote/start-candidate\x00" + path))
	return hex.EncodeToString(sum[:16])
}

func hidden(name string) bool { return strings.HasPrefix(name, ".") }

// isRepo accepts a .git directory or a .git file (worktree, submodule); a
// symlinked .git is not followed.
func isRepo(dir string) bool {
	info, err := os.Lstat(filepath.Join(dir, ".git"))
	return err == nil && (info.IsDir() || info.Mode().IsRegular())
}

// realDir resolves path and reports whether it is a directory.
func realDir(path string) (string, bool) {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", false
	}
	info, err := os.Stat(real)
	return real, err == nil && info.IsDir()
}

// rootRepos lists the root itself when it is a Git repo, otherwise the Git
// repos directly under it. A symlinked entry counts only when its real path is
// still a direct, non-hidden child of the root.
func rootRepos(root string) []string {
	if isRepo(root) {
		return []string{root}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		slog.Warn("project root unreadable", "error", err)
		return nil
	}
	var out []string
	for _, entry := range entries {
		if hidden(entry.Name()) {
			continue
		}
		real, ok := realDir(filepath.Join(root, entry.Name()))
		if !ok || !inRoot(root, real) || !isRepo(real) || slices.Contains(out, real) {
			continue
		}
		out = append(out, real)
	}
	return out
}

func inRoot(root, real string) bool {
	return real == root || filepath.Dir(real) == root && !hidden(filepath.Base(real))
}

// openWorkspaces maps the real cwd of each open workspace to it. A
// workspace's cwd is its first pane's cwd; the first workspace wins a tie.
func openWorkspaces(top herdr.Topology) (map[string]herdr.Workspace, []string) {
	first := map[string]string{}
	for _, pane := range top.Panes {
		if _, ok := first[pane.WorkspaceID]; !ok && pane.CWD != "" {
			first[pane.WorkspaceID] = pane.CWD
		}
	}
	workspaces := slices.Clone(top.Workspaces)
	slices.SortStableFunc(workspaces, func(a, b herdr.Workspace) int { return a.Number - b.Number })
	open := map[string]herdr.Workspace{}
	var order []string
	for _, ws := range workspaces {
		cwd, ok := first[ws.ID]
		if !ok || !filepath.IsAbs(cwd) {
			continue
		}
		real, ok := realDir(cwd)
		if !ok {
			continue
		}
		if _, dup := open[real]; dup {
			continue
		}
		open[real] = ws
		order = append(order, real)
	}
	return open, order
}

// Candidates merges the Git repos under each root with the open Herdr
// workspaces. A folder already open offers a new tab in that workspace.
func (s *Starter) Candidates(ctx context.Context) ([]Candidate, error) {
	top, err := s.gateway.Topology(ctx)
	if err != nil {
		return nil, err
	}
	open, order := openWorkspaces(top)
	var out []Candidate
	seen := map[string]bool{}
	add := func(c Candidate) {
		if len(out) >= maxCandidates || seen[c.path] {
			return
		}
		seen[c.path] = true
		c.ID = candidateID(c.path)
		c.Name = filepath.Base(c.path)
		c.Placement = PlacementNewWorkspace
		if ws, ok := open[c.path]; ok {
			c.Open, c.Workspace, c.workspaceID, c.Placement = true, ws.Label, ws.ID, PlacementNewTab
		}
		out = append(out, c)
	}
	for _, root := range s.roots {
		for _, repo := range rootRepos(root) {
			add(Candidate{path: repo, root: root, Root: filepath.Base(root)})
		}
	}
	for _, path := range order {
		add(Candidate{path: path})
	}
	return out, nil
}

var errCandidateChanged = errors.New("start candidate changed")

// revalidate re-resolves the folder immediately before dispatch: it must still
// be the same real directory and, for a root candidate, a Git repo in its root.
func revalidate(c Candidate) error {
	real, ok := realDir(c.path)
	if !ok || real != c.path {
		return errCandidateChanged
	}
	if c.root != "" && (!inRoot(c.root, real) || !isRepo(real)) {
		return errCandidateChanged
	}
	if c.Placement == PlacementNewTab && c.workspaceID == "" {
		return errCandidateChanged
	}
	return nil
}

// agentName derives the Herdr agent name from the command ID, so a second
// dispatch of one command would also collide on the live agent name.
func agentName(commandID string) string {
	return "mobile-" + strings.ReplaceAll(commandID, "-", "")[:12]
}

func rejectedStart(code string) StartResult { return StartResult{Status: "rejected", Code: code} }

// Start asks Herdr for a new session at the candidate. The caller's receipt
// guarantees one dispatch per command ID; Start never retries a create.
func (s *Starter) Start(ctx context.Context, commandID, candidate, kind, placement string) StartResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !slices.Contains(startKinds, kind) {
		return rejectedStart("UNSUPPORTED_KIND")
	}
	if len(strings.ReplaceAll(commandID, "-", "")) < 12 {
		return rejectedStart("INVALID_COMMAND_ID")
	}
	candidates, err := s.Candidates(ctx)
	if err != nil {
		// Only reads ran: nothing was created.
		if errors.Is(err, herdr.ErrUnsupported) {
			return rejectedStart("HERDR_UNSUPPORTED")
		}
		return rejectedStart("HERDR_UNAVAILABLE")
	}
	i := slices.IndexFunc(candidates, func(c Candidate) bool { return c.ID == candidate })
	if i < 0 || candidates[i].Placement != placement {
		return rejectedStart("CANDIDATE_CHANGED")
	}
	c := candidates[i]
	if revalidate(c) != nil {
		return rejectedStart("CANDIDATE_CHANGED")
	}
	var created herdr.Created
	if placement == PlacementNewTab {
		created, err = s.gateway.TabCreate(ctx, c.workspaceID, c.path)
	} else {
		created, err = s.gateway.WorkspaceCreate(ctx, c.path)
	}
	if err != nil {
		var api *herdr.APIError
		switch {
		case errors.Is(err, herdr.ErrUnsupported):
			return rejectedStart("HERDR_UNSUPPORTED")
		case errors.As(err, &api):
			slog.Warn("session start rejected by Herdr", "stage", placement, "code", api.Code)
			return rejectedStart("HERDR_REJECTED")
		}
		slog.Warn("session start delivery uncertain", "stage", placement, "error", err)
		return StartResult{Status: "delivery_unknown", Code: "DELIVERY_UNKNOWN"}
	}
	name := agentName(commandID)
	out := StartResult{Created: &created, Agent: name}
	err = s.gateway.StartAgent(ctx, name, kind, created.PaneID)
	var api *herdr.APIError
	switch {
	case err == nil:
		out.Status = "accepted"
	case errors.Is(err, herdr.ErrAgentNotReady):
		out.Status, out.Code = "accepted", "AGENT_NOT_READY"
	case errors.Is(err, herdr.ErrAgentUnconfirmed):
		out.Status, out.Code = "accepted", "AGENT_START_UNCONFIRMED"
	case errors.Is(err, herdr.ErrAgentExited):
		out.Status, out.Code = "rejected", "AGENT_START_FAILED"
	case errors.As(err, &api):
		slog.Warn("agent start rejected by Herdr", "code", api.Code)
		out.Status, out.Code = "rejected", "AGENT_START_FAILED"
	default:
		slog.Warn("agent start delivery uncertain", "error", err)
		out.Status, out.Code = "delivery_unknown", "DELIVERY_UNKNOWN"
	}
	return out
}
