package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/psw7205/herdr-remote/internal/herdr"
)

type fakeStartGateway struct {
	mu        sync.Mutex
	top       herdr.Topology
	topErr    error
	createErr error
	startErr  error
	calls     []string
}

func (f *fakeStartGateway) record(call string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
}
func (f *fakeStartGateway) Topology(context.Context) (herdr.Topology, error) {
	return f.top, f.topErr
}
func (f *fakeStartGateway) WorkspaceCreate(_ context.Context, cwd string) (herdr.Created, error) {
	f.record("workspace.create " + cwd)
	if f.createErr != nil {
		return herdr.Created{}, f.createErr
	}
	return herdr.Created{WorkspaceID: "w9", TabID: "w9:t1", PaneID: "w9:p1"}, nil
}
func (f *fakeStartGateway) TabCreate(_ context.Context, workspace, cwd string) (herdr.Created, error) {
	f.record("tab.create " + workspace + " " + cwd)
	if f.createErr != nil {
		return herdr.Created{}, f.createErr
	}
	return herdr.Created{WorkspaceID: workspace, TabID: workspace + ":t5", PaneID: workspace + ":p8"}, nil
}
func (f *fakeStartGateway) StartAgent(_ context.Context, name, kind, pane string) error {
	f.record("agent.start " + name + " " + kind + " " + pane)
	return f.startErr
}

const commandID = "0f8fad5b-d9cb-469f-a165-70867728950e"

func realTemp(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func mkdirs(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func names(cs []Candidate) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.Name)
	}
	return out
}

func newTestStarter(t *testing.T, gw *fakeStartGateway, roots ...string) *Starter {
	t.Helper()
	s, err := NewStarter(gw, roots)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRootThatIsARepoIsTheOnlyCandidate(t *testing.T) {
	root := realTemp(t)
	mkdirs(t, filepath.Join(root, ".git"), filepath.Join(root, "sub", ".git"))
	cs, err := newTestStarter(t, &fakeStartGateway{}, root).Candidates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 1 || cs[0].path != root || cs[0].Placement != PlacementNewWorkspace || cs[0].Open {
		t.Fatalf("candidates = %+v", cs)
	}
}

func TestOneLevelReposExcludeHiddenPlainNestedAndEscapes(t *testing.T) {
	root := realTemp(t)
	outside := realTemp(t)
	mkdirs(t,
		filepath.Join(root, "api", ".git"),
		filepath.Join(root, ".dotfiles", ".git"),
		filepath.Join(root, "plain"),
		filepath.Join(root, "group", "nested", ".git"),
		filepath.Join(outside, "evil", ".git"),
	)
	// A worktree or submodule has a .git file.
	mkdirs(t, filepath.Join(root, "worktree"))
	if err := os.WriteFile(filepath.Join(root, "worktree", ".git"), []byte("gitdir: elsewhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{
		"escape":      filepath.Join(outside, "evil"),
		"alias":       filepath.Join(root, "api"),
		"deep":        filepath.Join(root, "group", "nested"),
		"hidden-link": filepath.Join(root, ".dotfiles"),
	} {
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	cs, err := newTestStarter(t, &fakeStartGateway{}, root).Candidates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := names(cs); !slices.Equal(got, []string{"api", "worktree"}) {
		t.Fatalf("candidates = %v", got)
	}
	for _, c := range cs {
		if c.Root != filepath.Base(root) || c.ID == "" || c.ID == c.path {
			t.Fatalf("candidate = %+v", c)
		}
	}
}

func TestOpenWorkspaceMergesByRealCwdAndOffersNewTab(t *testing.T) {
	root := realTemp(t)
	elsewhere := realTemp(t)
	mkdirs(t, filepath.Join(root, "api", ".git"), filepath.Join(root, "web", ".git"))
	link := filepath.Join(elsewhere, "api-link")
	if err := os.Symlink(filepath.Join(root, "api"), link); err != nil {
		t.Fatal(err)
	}
	gw := &fakeStartGateway{top: herdr.Topology{
		Workspaces: []herdr.Workspace{{ID: "w2", Number: 2, Label: "api again"}, {ID: "w1", Number: 1, Label: "api"}, {ID: "w3", Number: 3, Label: "notes"}},
		Panes: []herdr.Pane{
			{PaneID: "w2:p1", WorkspaceID: "w2", CWD: filepath.Join(root, "api")},
			{PaneID: "w1:p1", WorkspaceID: "w1", CWD: link},
			// Only the first pane names a workspace's folder.
			{PaneID: "w1:p2", WorkspaceID: "w1", CWD: filepath.Join(root, "web")},
			{PaneID: "w3:p1", WorkspaceID: "w3", CWD: elsewhere},
		},
	}}
	cs, err := newTestStarter(t, gw, root).Candidates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := names(cs); !slices.Equal(got, []string{"api", "web", filepath.Base(elsewhere)}) {
		t.Fatalf("candidates = %v", got)
	}
	api, web, notes := cs[0], cs[1], cs[2]
	if !api.Open || api.workspaceID != "w1" || api.Workspace != "api" || api.Placement != PlacementNewTab {
		t.Fatalf("open repo = %+v", api)
	}
	if web.Open || web.Placement != PlacementNewWorkspace {
		t.Fatalf("closed repo = %+v", web)
	}
	if !notes.Open || notes.Root != "" || notes.workspaceID != "w3" || notes.Placement != PlacementNewTab {
		t.Fatalf("open workspace outside roots = %+v", notes)
	}
}

func TestNoRootOffersOnlyOpenWorkspaces(t *testing.T) {
	dir := realTemp(t)
	gw := &fakeStartGateway{top: herdr.Topology{Workspaces: []herdr.Workspace{{ID: "w1", Number: 1}}, Panes: []herdr.Pane{{PaneID: "w1:p1", WorkspaceID: "w1", CWD: dir}}}}
	s := newTestStarter(t, gw)
	if s.NewWorkspace() {
		t.Fatal("new workspace enabled without a root")
	}
	cs, _ := s.Candidates(context.Background())
	if len(cs) != 1 || cs[0].Placement != PlacementNewTab {
		t.Fatalf("candidates = %+v", cs)
	}
}

func TestCandidateIDIsStableAndOpaque(t *testing.T) {
	if candidateID("/work/api") != candidateID("/work/api") || candidateID("/work/api") == candidateID("/work/web") {
		t.Fatal("candidate IDs are not stable per path")
	}
}

func TestNewStarterRejectsRelativeOrMissingRoot(t *testing.T) {
	for _, root := range []string{"relative/root", filepath.Join(t.TempDir(), "missing")} {
		if _, err := NewStarter(&fakeStartGateway{}, []string{root}); err == nil {
			t.Fatalf("accepted root %q", root)
		}
	}
}

func TestStartCreatesWorkspaceAndStartsClaudeWithoutArgs(t *testing.T) {
	root := realTemp(t)
	mkdirs(t, filepath.Join(root, "api", ".git"))
	gw := &fakeStartGateway{}
	s := newTestStarter(t, gw, root)
	cs, _ := s.Candidates(context.Background())
	got := s.Start(context.Background(), commandID, cs[0].ID, "claude", PlacementNewWorkspace)
	if got.Status != "accepted" || got.Code != "" || got.Created == nil || got.Created.PaneID != "w9:p1" || got.Agent != "mobile-0f8fad5bd9cb" {
		t.Fatalf("result = %+v", got)
	}
	want := []string{"workspace.create " + filepath.Join(root, "api"), "agent.start mobile-0f8fad5bd9cb claude w9:p1"}
	if !slices.Equal(gw.calls, want) {
		t.Fatalf("calls = %v", gw.calls)
	}
}

func TestStartOpensTabInOpenWorkspace(t *testing.T) {
	root := realTemp(t)
	api := filepath.Join(root, "api")
	mkdirs(t, filepath.Join(api, ".git"))
	gw := &fakeStartGateway{top: herdr.Topology{Workspaces: []herdr.Workspace{{ID: "w1", Number: 1}}, Panes: []herdr.Pane{{PaneID: "w1:p1", WorkspaceID: "w1", CWD: api}}}}
	s := newTestStarter(t, gw, root)
	cs, _ := s.Candidates(context.Background())
	if got := s.Start(context.Background(), commandID, cs[0].ID, "claude", PlacementNewWorkspace); got.Status != "rejected" || got.Code != "CANDIDATE_CHANGED" {
		t.Fatalf("duplicate workspace for an open folder: %+v", got)
	}
	got := s.Start(context.Background(), commandID, cs[0].ID, "claude", PlacementNewTab)
	if got.Status != "accepted" || got.Created.WorkspaceID != "w1" || got.Created.PaneID != "w1:p8" {
		t.Fatalf("result = %+v", got)
	}
	if gw.calls[0] != "tab.create w1 "+api {
		t.Fatalf("calls = %v", gw.calls)
	}
}

func TestStartRevalidatesBeforeDispatch(t *testing.T) {
	root := realTemp(t)
	outside := realTemp(t)
	mkdirs(t, filepath.Join(root, "api", ".git"), filepath.Join(root, "web", ".git"), filepath.Join(outside, "evil", ".git"))
	gw := &fakeStartGateway{}
	s := newTestStarter(t, gw, root)
	cs, _ := s.Candidates(context.Background())

	// Removed after listing.
	if err := os.RemoveAll(filepath.Join(root, "api")); err != nil {
		t.Fatal(err)
	}
	if got := s.Start(context.Background(), commandID, cs[0].ID, "claude", PlacementNewWorkspace); got.Code != "CANDIDATE_CHANGED" {
		t.Fatalf("removed folder: %+v", got)
	}
	// Retargeted outside the root after listing.
	if err := os.RemoveAll(filepath.Join(root, "web")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "evil"), filepath.Join(root, "web")); err != nil {
		t.Fatal(err)
	}
	if got := s.Start(context.Background(), commandID, cs[1].ID, "claude", PlacementNewWorkspace); got.Code != "CANDIDATE_CHANGED" {
		t.Fatalf("escaped folder: %+v", got)
	}
	if len(gw.calls) != 0 {
		t.Fatalf("dispatched after failed revalidation: %v", gw.calls)
	}
}

func TestRevalidateRejectsChangedPath(t *testing.T) {
	root := realTemp(t)
	api := filepath.Join(root, "api")
	mkdirs(t, filepath.Join(api, ".git"))
	c := Candidate{path: api, root: root, Placement: PlacementNewWorkspace}
	if err := revalidate(c); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(api, ".git")); err != nil {
		t.Fatal(err)
	}
	if revalidate(c) == nil {
		t.Fatal("accepted a folder that is no longer a repo")
	}
	if revalidate(Candidate{path: filepath.Join(root, "gone")}) == nil {
		t.Fatal("accepted a missing folder")
	}
}

func TestStartRejectsUnknownKindAndCandidate(t *testing.T) {
	root := realTemp(t)
	mkdirs(t, filepath.Join(root, "api", ".git"))
	gw := &fakeStartGateway{}
	s := newTestStarter(t, gw, root)
	cs, _ := s.Candidates(context.Background())
	if got := s.Start(context.Background(), commandID, cs[0].ID, "codex", PlacementNewWorkspace); got.Code != "UNSUPPORTED_KIND" {
		t.Fatalf("kind: %+v", got)
	}
	if got := s.Start(context.Background(), commandID, "unknown", "claude", PlacementNewWorkspace); got.Code != "CANDIDATE_CHANGED" {
		t.Fatalf("candidate: %+v", got)
	}
	gw.topErr = errors.New("socket closed")
	if got := s.Start(context.Background(), commandID, cs[0].ID, "claude", PlacementNewWorkspace); got.Status != "rejected" || got.Code != "HERDR_UNAVAILABLE" {
		t.Fatalf("herdr down: %+v", got)
	}
	if len(gw.calls) != 0 {
		t.Fatalf("calls = %v", gw.calls)
	}
}

func TestStartOutcomeKeepsCreatedTopology(t *testing.T) {
	root := realTemp(t)
	mkdirs(t, filepath.Join(root, "api", ".git"))
	for name, tc := range map[string]struct {
		createErr, startErr error
		status, code        string
		created             bool
	}{
		"trust_screen":       {nil, herdr.ErrAgentNotReady, "accepted", "AGENT_NOT_READY", true},
		"unconfirmed":        {nil, herdr.ErrAgentUnconfirmed, "accepted", "AGENT_START_UNCONFIRMED", true},
		"exited":             {nil, herdr.ErrAgentExited, "rejected", "AGENT_START_FAILED", true},
		"start_rejected":     {nil, &herdr.APIError{Code: "agent_pane_busy"}, "rejected", "AGENT_START_FAILED", true},
		"start_lost":         {nil, context.DeadlineExceeded, "delivery_unknown", "DELIVERY_UNKNOWN", true},
		"create_rejected":    {&herdr.APIError{Code: "invalid_cwd"}, nil, "rejected", "HERDR_REJECTED", false},
		"create_lost":        {context.DeadlineExceeded, nil, "delivery_unknown", "DELIVERY_UNKNOWN", false},
		"create_unsupported": {herdr.ErrUnsupported, nil, "rejected", "HERDR_UNSUPPORTED", false},
	} {
		t.Run(name, func(t *testing.T) {
			gw := &fakeStartGateway{createErr: tc.createErr, startErr: tc.startErr}
			s := newTestStarter(t, gw, root)
			cs, _ := s.Candidates(context.Background())
			got := s.Start(context.Background(), commandID, cs[0].ID, "claude", PlacementNewWorkspace)
			if got.Status != tc.status || got.Code != tc.code || (got.Created != nil) != tc.created {
				t.Fatalf("result = %+v", got)
			}
			// A failed start never retries what Herdr created.
			if n := len(gw.calls); tc.created && n != 2 || !tc.created && n != 1 {
				t.Fatalf("calls = %v", gw.calls)
			}
		})
	}
}
