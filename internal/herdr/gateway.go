// Package herdr implements the inspected Herdr JSON socket API.
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

// ErrUnsupported means the Herdr server rejected the method name itself, so
// the request reached no handler.
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

// Binding identifies one live native process/session in a pane's foreground;
// the Bridge verifies it (internal/binding), Herdr does not issue it. A pane ID
// or terminal ID alone is never sufficient for a write.
type Binding struct {
	Token           string
	TerminalID      string
	NativeSessionID string
	ProcessID       uint32
	Agent           string
	CWD             string
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

// AgentInfo reads agent.get for one pane. Its terminal_id names the terminal
// Herdr currently runs in that pane.
func (g *Gateway) AgentInfo(ctx context.Context, paneID string) (Agent, error) {
	if paneID == "" {
		return Agent{}, ErrEmptyTarget
	}
	var result struct {
		Type  string `json:"type"`
		Agent *Agent `json:"agent"`
	}
	if err := g.read(ctx, "agent.get", map[string]string{"target": paneID}, &result); err != nil {
		return Agent{}, err
	}
	if result.Type != "agent_info" || result.Agent == nil || result.Agent.PaneID != paneID || result.Agent.TerminalID == "" {
		return Agent{}, errors.New("agent response target mismatch")
	}
	return *result.Agent, nil
}

// SendInput is pane.send_input: Herdr wraps text in bracketed paste when the
// program enabled it, encodes keys for the terminal's keyboard protocol, and
// rejects the whole submission before queueing it when the PTY's canonical
// line limit would truncate it.
func (g *Gateway) SendInput(ctx context.Context, paneID, text string, keys []string) error {
	if paneID == "" {
		return ErrEmptyTarget
	}
	if text == "" && len(keys) == 0 {
		return errors.New("empty input")
	}
	params := map[string]any{"pane_id": paneID}
	if text != "" {
		params["text"] = text
	}
	if len(keys) > 0 {
		params["keys"] = keys
	}
	return g.write(ctx, "pane.send_input", params)
}

// SendKeys is pane.send_keys: each key name is encoded for the terminal's
// keyboard protocol.
func (g *Gateway) SendKeys(ctx context.Context, paneID string, keys []string) error {
	if paneID == "" {
		return ErrEmptyTarget
	}
	if len(keys) == 0 {
		return errors.New("empty input")
	}
	return g.write(ctx, "pane.send_keys", map[string]any{"pane_id": paneID, "keys": keys})
}

// SendText is pane.send_text: Herdr writes the bytes to the PTY verbatim.
func (g *Gateway) SendText(ctx context.Context, paneID, text string) error {
	if paneID == "" {
		return ErrEmptyTarget
	}
	if text == "" {
		return errors.New("empty input")
	}
	return g.write(ctx, "pane.send_text", map[string]string{"pane_id": paneID, "text": text})
}

func (g *Gateway) write(ctx context.Context, method string, params any) error {
	var result struct {
		Type string `json:"type"`
	}
	if err := g.read(ctx, method, params, &result); err != nil {
		return err
	}
	if result.Type != "ok" {
		return errors.New("invalid " + method + " response")
	}
	return nil
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
	// Herdr rejects an unknown method while deserializing the request. 0.9.1
	// answers without an ID; 0.9.2 and later echo the request ID. Only that
	// exact rejection proves the method is absent.
	if (response.ID == "" || response.ID == id) && response.Error != nil && response.Error.Code == "invalid_request" && strings.HasPrefix(response.Error.Message, "invalid request: unknown variant `"+method+"`") {
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
