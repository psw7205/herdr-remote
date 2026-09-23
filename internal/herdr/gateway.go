// Package herdr implements the inspected, public JSON socket read API.
// It deliberately exposes no generic RPC or mutation method: protocol 22 does
// not provide the conditional input contract required by the mobile bridge.
package herdr

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync/atomic"
	"time"
)

const maxResponseBytes = 4 << 20

var ErrEmptyTarget = errors.New("explicit pane target required")
var requestSequence atomic.Uint64

type Gateway struct{ socket string }

func NewGateway(socket string) *Gateway { return &Gateway{socket: socket} }

type NativeSession struct {
	Source string `json:"source"`
	Agent  string `json:"agent"`
	Kind   string `json:"kind"`
	Value  string `json:"value"`
}

type Agent struct {
	TerminalID  string         `json:"terminal_id"`
	PaneID      string         `json:"pane_id"`
	TabID       string         `json:"tab_id"`
	WorkspaceID string         `json:"workspace_id"`
	Agent       string         `json:"agent"`
	CWD         string         `json:"cwd"`
	Title       string         `json:"terminal_title_stripped"`
	Status      string         `json:"agent_status"`
	Session     *NativeSession `json:"agent_session,omitempty"`
}

type Snapshot struct {
	Version  string  `json:"version"`
	Protocol int     `json:"protocol"`
	Agents   []Agent `json:"agents"`
}

type Process struct {
	PID   uint32 `json:"pid"`
	Name  string `json:"name"`
	Argv0 string `json:"argv0"`
}

type ProcessInfo struct {
	PaneID                   string    `json:"pane_id"`
	ShellPID                 *uint32   `json:"shell_pid,omitempty"`
	ForegroundProcessGroupID *uint32   `json:"foreground_process_group_id,omitempty"`
	ForegroundProcesses      []Process `json:"foreground_processes"`
}

type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// TerminalSnapshot is a complete visible frame, not an append-only byte stream.
// Consumers replace their frame instead of appending it to terminal history.
type TerminalSnapshot struct {
	PaneID    string `json:"pane_id"`
	Source    string `json:"source"`
	Format    string `json:"format"`
	Text      string `json:"text"`
	Truncated bool   `json:"truncated"`
}

// Binding is issued by the Herdr server for one live native process/session.
// A pane ID or terminal ID alone is never sufficient for a write.
type Binding struct {
	Token           string `json:"token"`
	TerminalID      string `json:"terminal_id"`
	NativeSessionID string `json:"native_session_id"`
	ProcessID       uint32 `json:"process_id"`
	Agent           string `json:"agent"`
	CWD             string `json:"cwd"`
}

func (g *Gateway) Binding(ctx context.Context, paneID string) (Binding, error) {
	if paneID == "" {
		return Binding{}, ErrEmptyTarget
	}
	var result struct {
		Type    string   `json:"type"`
		Binding *Binding `json:"binding"`
	}
	if err := g.read(ctx, "agent.binding", map[string]string{"target": paneID}, &result); err != nil {
		return Binding{}, err
	}
	if result.Type != "agent_binding" || result.Binding == nil || result.Binding.Token == "" || result.Binding.NativeSessionID == "" || result.Binding.ProcessID == 0 || result.Binding.TerminalID == "" {
		return Binding{}, errors.New("invalid native runtime binding")
	}
	return *result.Binding, nil
}

func (g *Gateway) BoundInput(ctx context.Context, paneID, binding, kind, text string) error {
	if paneID == "" {
		return ErrEmptyTarget
	}
	if binding == "" {
		return errors.New("native runtime binding required")
	}
	if kind != "prompt" && kind != "interrupt" && kind != "terminal_input" {
		return errors.New("unsupported bound input type")
	}
	var result struct {
		Type string `json:"type"`
	}
	params := map[string]any{"target": paneID, "binding": binding, "input": map[string]string{"type": kind, "text": text}}
	if err := g.read(ctx, "agent.bound_input", params, &result); err != nil {
		return err
	}
	if result.Type != "ok" {
		return errors.New("invalid bound input response")
	}
	return nil
}

func (g *Gateway) TerminalSnapshot(ctx context.Context, paneID string) (TerminalSnapshot, error) {
	if paneID == "" {
		return TerminalSnapshot{}, ErrEmptyTarget
	}
	var result struct {
		Type string            `json:"type"`
		Read *TerminalSnapshot `json:"read"`
	}
	// Visible ANSI cannot trigger Herdr's interactive alternate-screen history
	// collection. Never switch to recent/recent_unwrapped to obtain more lines.
	params := map[string]any{"pane_id": paneID, "source": "visible", "format": "ansi", "strip_ansi": false}
	if err := g.read(ctx, "pane.read", params, &result); err != nil {
		return TerminalSnapshot{}, err
	}
	if result.Type != "pane_read" || result.Read == nil || result.Read.PaneID != paneID || result.Read.Source != "visible" || result.Read.Format != "ansi" {
		return TerminalSnapshot{}, errors.New("invalid visible ANSI terminal snapshot")
	}
	return *result.Read, nil
}

func (e *APIError) Error() string { return e.Code + ": " + e.Message }

func (g *Gateway) Snapshot(ctx context.Context) (Snapshot, error) {
	var result struct {
		Type     string    `json:"type"`
		Snapshot *Snapshot `json:"snapshot"`
	}
	if err := g.read(ctx, "session.snapshot", struct{}{}, &result); err != nil {
		return Snapshot{}, err
	}
	if result.Type != "session_snapshot" || result.Snapshot == nil || result.Snapshot.Version == "" || result.Snapshot.Protocol == 0 {
		return Snapshot{}, errors.New("invalid session snapshot response")
	}
	return *result.Snapshot, nil
}

func (g *Gateway) ProcessInfo(ctx context.Context, paneID string) (ProcessInfo, error) {
	if paneID == "" {
		return ProcessInfo{}, ErrEmptyTarget
	}
	var result struct {
		Type string       `json:"type"`
		Info *ProcessInfo `json:"process_info"`
	}
	if err := g.read(ctx, "pane.process_info", map[string]string{"pane_id": paneID}, &result); err != nil {
		return ProcessInfo{}, err
	}
	if result.Type != "pane_process_info" || result.Info == nil || result.Info.PaneID != paneID {
		return ProcessInfo{}, errors.New("process response target mismatch")
	}
	return *result.Info, nil
}

// A request uses one connection, a bounded response and a bounded lifetime.
// No retry can silently turn a read failure into another operation.
func (g *Gateway) read(ctx context.Context, method string, params any, result any) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	c, err := (&net.Dialer{}).DialContext(ctx, "unix", g.socket)
	if err != nil {
		return err
	}
	defer c.Close()
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	if err := c.SetDeadline(deadline); err != nil {
		return err
	}
	id := fmt.Sprintf("mobile-read-%d", requestSequence.Add(1))
	request := struct {
		ID     string `json:"id"`
		Method string `json:"method"`
		Params any    `json:"params"`
	}{id, method, params}
	if err := json.NewEncoder(c).Encode(request); err != nil {
		return contextError(ctx, err)
	}
	line, err := bufio.NewReader(io.LimitReader(c, maxResponseBytes+1)).ReadBytes('\n')
	if len(line) > maxResponseBytes {
		return errors.New("Herdr response exceeds size limit")
	}
	if err != nil {
		return contextError(ctx, err)
	}
	var response struct {
		ID     string          `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  *APIError       `json:"error"`
	}
	if err := json.Unmarshal(line, &response); err != nil {
		return fmt.Errorf("decode Herdr response: %w", err)
	}
	if response.ID != id {
		return errors.New("Herdr response correlation mismatch")
	}
	if response.Error != nil {
		return response.Error
	}
	if len(response.Result) == 0 || string(response.Result) == "null" {
		return errors.New("missing Herdr result")
	}
	return json.Unmarshal(response.Result, result)
}

func contextError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	// A socket deadline can fire just before the context timer runs.
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return context.DeadlineExceeded
	}
	return err
}
