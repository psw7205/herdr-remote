package herdr

import (
	"context"
	"errors"
	"time"
)

// Topology is the part of session.snapshot that describes open workspaces.
// workspace.list carries no cwd, so a workspace's directory is read from its
// panes; one snapshot keeps workspaces and panes consistent with each other.
type Topology struct {
	Workspaces []Workspace
	Panes      []Pane
}

type Workspace struct {
	ID     string `json:"workspace_id"`
	Number int    `json:"number"`
	Label  string `json:"label"`
}

type Pane struct {
	PaneID      string `json:"pane_id"`
	WorkspaceID string `json:"workspace_id"`
	TabID       string `json:"tab_id"`
	CWD         string `json:"cwd"`
}

// Created names the topology Herdr made for a new session. PaneID is the root
// pane of the new tab, where the agent is started.
type Created struct {
	WorkspaceID string `json:"workspace_id"`
	TabID       string `json:"tab_id"`
	PaneID      string `json:"pane_id"`
}

func (g *Gateway) Topology(ctx context.Context) (Topology, error) {
	var result struct {
		Type     string `json:"type"`
		Snapshot *struct {
			Workspaces []Workspace `json:"workspaces"`
			Panes      []Pane      `json:"panes"`
		} `json:"snapshot"`
	}
	if err := g.read(ctx, "session.snapshot", struct{}{}, &result); err != nil {
		return Topology{}, err
	}
	if result.Type != "session_snapshot" || result.Snapshot == nil || result.Snapshot.Workspaces == nil || result.Snapshot.Panes == nil {
		return Topology{}, errors.New("invalid session topology response")
	}
	return Topology{Workspaces: result.Snapshot.Workspaces, Panes: result.Snapshot.Panes}, nil
}

type createdResult struct {
	Type      string     `json:"type"`
	Workspace *Workspace `json:"workspace"`
	Tab       *struct {
		TabID       string `json:"tab_id"`
		WorkspaceID string `json:"workspace_id"`
	} `json:"tab"`
	RootPane *Pane `json:"root_pane"`
}

// WorkspaceCreate opens a workspace at cwd without changing focus or passing
// env. The cwd must be absolute.
func (g *Gateway) WorkspaceCreate(ctx context.Context, cwd string) (Created, error) {
	if cwd == "" {
		return Created{}, ErrEmptyTarget
	}
	var result createdResult
	params := map[string]any{"cwd": cwd, "focus": false}
	if err := g.read(ctx, "workspace.create", params, &result); err != nil {
		return Created{}, err
	}
	if result.Type != "workspace_created" || result.Workspace == nil || result.Tab == nil || result.RootPane == nil ||
		result.Workspace.ID == "" || result.Tab.WorkspaceID != result.Workspace.ID || result.RootPane.TabID != result.Tab.TabID || result.RootPane.PaneID == "" {
		return Created{}, errors.New("invalid workspace create response")
	}
	return Created{WorkspaceID: result.Workspace.ID, TabID: result.Tab.TabID, PaneID: result.RootPane.PaneID}, nil
}

// TabCreate opens a tab at cwd in an existing workspace without changing
// focus or passing env.
func (g *Gateway) TabCreate(ctx context.Context, workspaceID, cwd string) (Created, error) {
	if workspaceID == "" || cwd == "" {
		return Created{}, ErrEmptyTarget
	}
	var result createdResult
	params := map[string]any{"workspace_id": workspaceID, "cwd": cwd, "focus": false}
	if err := g.read(ctx, "tab.create", params, &result); err != nil {
		return Created{}, err
	}
	if result.Type != "tab_created" || result.Tab == nil || result.RootPane == nil ||
		result.Tab.WorkspaceID != workspaceID || result.RootPane.TabID != result.Tab.TabID || result.RootPane.PaneID == "" {
		return Created{}, errors.New("invalid tab create response")
	}
	return Created{WorkspaceID: workspaceID, TabID: result.Tab.TabID, PaneID: result.RootPane.PaneID}, nil
}

// AgentState is the part of agent.get that decides startup readiness.
type AgentState struct {
	TerminalID       string `json:"terminal_id"`
	PaneID           string `json:"pane_id"`
	Name             string `json:"name"`
	Agent            string `json:"agent"`
	Status           string `json:"agent_status"`
	InteractiveReady bool   `json:"interactive_ready"`
	LaunchPending    bool   `json:"launch_pending"`
}

func (g *Gateway) agentStart(ctx context.Context, name, kind, paneID string) (AgentState, error) {
	var result struct {
		Type  string      `json:"type"`
		Agent *AgentState `json:"agent"`
	}
	// No args and no timeout_ms: arguments after `--` could resume a session
	// or bypass permissions, and Herdr's default startup deadline is 30s.
	params := map[string]string{"name": name, "kind": kind, "pane_id": paneID}
	if err := g.read(ctx, "agent.start", params, &result); err != nil {
		return AgentState{}, err
	}
	if result.Type != "agent_started" || result.Agent == nil || result.Agent.TerminalID == "" || result.Agent.PaneID != paneID {
		return AgentState{}, errors.New("invalid agent start response")
	}
	return *result.Agent, nil
}

func (g *Gateway) agentGet(ctx context.Context, target string) (AgentState, error) {
	var result struct {
		Type  string      `json:"type"`
		Agent *AgentState `json:"agent"`
	}
	if err := g.read(ctx, "agent.get", map[string]string{"target": target}, &result); err != nil {
		return AgentState{}, err
	}
	if result.Type != "agent_info" || result.Agent == nil {
		return AgentState{}, errors.New("invalid agent response")
	}
	return *result.Agent, nil
}

// Startup outcomes observed after Herdr accepted agent.start. The launch
// input has been sent in all three cases.
var (
	// ErrAgentNotReady: the agent is blocked on a startup screen such as the
	// folder trust prompt and waits for a person at the PC or Terminal.
	ErrAgentNotReady = errors.New("agent blocked during startup")
	// ErrAgentExited: the agent process ended before it became interactive.
	ErrAgentExited = errors.New("agent exited before becoming interactive")
	// ErrAgentUnconfirmed: readiness could not be observed before the deadline,
	// or the pane no longer holds the named agent.
	ErrAgentUnconfirmed = errors.New("agent startup not confirmed")
)

// Startup timing mirrors the Herdr CLI's `agent start`: the server reports
// agent_pane_busy while a fresh pane's shell initializes, and readiness is
// observed by polling agent.get until Herdr's own 30s startup deadline.
var (
	agentStartWait      = 31 * time.Second
	agentPollInterval   = 200 * time.Millisecond
	shellReadyRetry     = 2 * time.Second
	shellReadyRetryStep = 100 * time.Millisecond
)

// StartAgent asks Herdr to start kind in the shell of paneID and waits until
// the agent is interactive. An *APIError or transport error means the
// agent.start request itself failed; after Herdr accepts it the result is nil
// or one of ErrAgentNotReady, ErrAgentExited and ErrAgentUnconfirmed.
func (g *Gateway) StartAgent(ctx context.Context, name, kind, paneID string) error {
	if paneID == "" || name == "" || kind == "" {
		return ErrEmptyTarget
	}
	started, err := g.startWhenShellReady(ctx, name, kind, paneID)
	if err != nil {
		return err
	}
	return g.waitInteractive(ctx, name, kind, paneID, started.TerminalID)
}

func (g *Gateway) startWhenShellReady(ctx context.Context, name, kind, paneID string) (AgentState, error) {
	deadline := time.Now().Add(shellReadyRetry)
	for {
		started, err := g.agentStart(ctx, name, kind, paneID)
		var api *APIError
		// agent_pane_busy is returned before any input is sent. It is retried
		// only while the pane's shell is still the initializing foreground.
		if err == nil || !errors.As(err, &api) || api.Code != "agent_pane_busy" || !time.Now().Before(deadline) || !g.shellInitializing(ctx, paneID) {
			return started, err
		}
		select {
		case <-ctx.Done():
			return AgentState{}, err
		case <-time.After(shellReadyRetryStep):
		}
	}
}

func (g *Gateway) shellInitializing(ctx context.Context, paneID string) bool {
	info, err := g.ProcessInfo(ctx, paneID)
	if err != nil || info.ShellPID == nil || info.ForegroundProcessGroupID == nil || *info.ForegroundProcessGroupID != *info.ShellPID {
		return false
	}
	for _, process := range info.ForegroundProcesses {
		if process.PID == *info.ShellPID {
			return true
		}
	}
	return false
}

func (g *Gateway) waitInteractive(ctx context.Context, name, kind, paneID, terminalID string) error {
	ctx, cancel := context.WithTimeout(ctx, agentStartWait)
	defer cancel()
	for {
		state, err := g.agentGet(ctx, name)
		if err != nil {
			// A name can be briefly unresolvable; the pane still identifies
			// the terminal, which is pinned below.
			state, err = g.agentGet(ctx, paneID)
		}
		if err == nil {
			switch {
			case state.TerminalID != terminalID || state.Name != name || state.Agent != "" && state.Agent != kind:
				return ErrAgentUnconfirmed
			case state.Status == "blocked":
				return ErrAgentNotReady
			case (state.Status == "idle" || state.Status == "done") && state.InteractiveReady:
				return nil
			case (state.Status == "idle" || state.Status == "done") && !state.LaunchPending:
				return ErrAgentExited
			}
		}
		select {
		case <-ctx.Done():
			return ErrAgentUnconfirmed
		case <-time.After(agentPollInterval):
		}
	}
}
