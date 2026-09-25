// Package herdr implements the inspected Herdr JSON socket API. Stock Herdr
// provides no conditional input; the mobile-binding patch adds agent.binding
// and agent.bound_input, and ErrUnsupported tells the two builds apart.
package herdr

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"time"
)

const maxResponseBytes = 4 << 20

var ErrEmptyTarget = errors.New("explicit pane target required")

// ErrUnsupported means the Herdr server rejected the method name itself, as a
// stock build does for agent.binding and agent.bound_input.
var ErrUnsupported = errors.New("Herdr method unsupported")
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
	// Cols and Rows are the grid a client renders the frame into. They are not
	// part of pane.read; the Bridge fills them from PaneSize and leaves them
	// zero when the size is unknown.
	Cols int `json:"cols,omitempty"`
	Rows int `json:"rows,omitempty"`
}

// PaneSize is the display-cell size of a pane's layout rect. Herdr exposes no
// PTY cols/rows on its public API, so this is the outer rect from pane.layout:
// it includes pane borders and the scrollbar gutter and is an upper bound on the
// PTY grid (exact when the pane has neither). A direct attach resize lock can
// also make the PTY differ, so clients must still fit the frame's own lines.
type PaneSize struct {
	Cols int
	Rows int
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

// paneSizeTimeout bounds the pane.layout read. The size is only a render hint
// fetched on every terminal poll, so a slow layout read must not hold the frame
// for the generic request lifetime.
const paneSizeTimeout = 500 * time.Millisecond

// PaneSize reads pane.layout, which is read-only and never resizes the PTY.
func (g *Gateway) PaneSize(ctx context.Context, paneID string) (PaneSize, error) {
	if paneID == "" {
		return PaneSize{}, ErrEmptyTarget
	}
	ctx, cancel := context.WithTimeout(ctx, paneSizeTimeout)
	defer cancel()
	type rect struct {
		Width  int `json:"width"`
		Height int `json:"height"`
	}
	var result struct {
		Type   string `json:"type"`
		Layout *struct {
			Zoomed        bool   `json:"zoomed"`
			Area          rect   `json:"area"`
			FocusedPaneID string `json:"focused_pane_id"`
			Panes         []struct {
				PaneID string `json:"pane_id"`
				Rect   rect   `json:"rect"`
			} `json:"panes"`
		} `json:"layout"`
	}
	if err := g.read(ctx, "pane.layout", map[string]string{"pane_id": paneID}, &result); err != nil {
		return PaneSize{}, err
	}
	if result.Type != "pane_layout" || result.Layout == nil {
		return PaneSize{}, errors.New("invalid pane layout response")
	}
	layout := result.Layout
	for _, pane := range layout.Panes {
		if pane.PaneID != paneID {
			continue
		}
		r := pane.Rect
		// A zoomed tab renders only its focused pane, over the whole tab area.
		if layout.Zoomed && layout.FocusedPaneID == paneID {
			r = layout.Area
		}
		if r.Width <= 0 || r.Height <= 0 {
			break
		}
		return PaneSize{Cols: r.Width, Rows: r.Height}, nil
	}
	return PaneSize{}, errors.New("pane missing from layout")
}

func (e *APIError) Error() string { return e.Code + ": " + e.Message }

// Conditional input capability observed from agent.binding responses.
const (
	ConditionalInputUnknown     = "unknown"
	ConditionalInputSupported   = "supported"
	ConditionalInputUnsupported = "unsupported"
)

// ObserveConditionalInput folds one agent.binding result into current.
// Transport and response-shape errors are not evidence either way.
func ObserveConditionalInput(current string, err error) string {
	var api *APIError
	switch {
	case current == ConditionalInputUnsupported || errors.Is(err, ErrUnsupported):
		return ConditionalInputUnsupported
	case err == nil || errors.As(err, &api):
		return ConditionalInputSupported
	}
	return current
}

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
	// Herdr rejects an unknown method while deserializing the request, before
	// it has an ID to echo. Only that exact rejection proves the method is absent.
	if response.ID == "" && response.Error != nil && response.Error.Code == "invalid_request" && strings.HasPrefix(response.Error.Message, "invalid request: unknown variant `"+method+"`") {
		return fmt.Errorf("%w: %s", ErrUnsupported, method)
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
