// Package binding verifies which native Claude session runs in the foreground
// of a Herdr pane, and sends input through stock Herdr only while that
// verification still holds. Herdr owns the pane and PTY; the Bridge mints and
// checks the runtime binding token itself.
package binding

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/psw7205/herdr-remote/internal/herdr"
)

// ErrUnverified means the pane's foreground job is not a native Claude session
// whose identity the Bridge could verify.
var ErrUnverified = errors.New("no verified native runtime binding")

// ErrDeliveryUnknown means part or all of the input may have reached the PTY.
// The command must not be retried automatically.
var ErrDeliveryUnknown = errors.New("delivery unknown")

// Rejection codes. A Rejected error means nothing reached the PTY.
const (
	CodeBindingMismatch = "runtime_binding_mismatch"
	CodeSessionEnded    = "session_ended"
	CodeInvalidPrompt   = "invalid_prompt"
	CodeInvalidInput    = "invalid_input"
	CodeInputRejected   = "input_rejected"
)

type Rejected struct {
	Code string
	Err  error
}

func (e *Rejected) Error() string { return e.Code + ": " + e.Err.Error() }
func (e *Rejected) Unwrap() error { return e.Err }

const (
	maxMetadataBytes = 65536
	maxInputBytes    = 65536
)

// promptSettle is the pause between prompt text and Enter, the same as Herdr's
// own agent.prompt, so the agent has taken the pasted text before Enter.
var promptSettle = 300 * time.Millisecond

// procStartLayout is how Claude Code writes procStart in sessions/<pid>.json:
// the process start second in UTC, ctime style with a space-padded day.
const procStartLayout = "Mon Jan _2 15:04:05 2006"

// StartTime is the kernel's birth time of a process. A PID alone is not an
// incarnation identity; PID and start time together are.
type StartTime struct {
	Sec  int64
	Usec int64
}

func (s StartTime) micros() int64 { return s.Sec*1_000_000 + s.Usec }

var errStartTimeUnsupported = errors.New("process start time unsupported on this platform")

// Supported reports whether this platform can read process start times. Without
// them no binding is ever verified.
func Supported() bool { return startTimeSupported }

type herdrAPI interface {
	AgentInfo(context.Context, string) (herdr.Agent, error)
	ProcessInfo(context.Context, string) (herdr.ProcessInfo, error)
	SendInput(context.Context, string, string, []string) error
	SendKeys(context.Context, string, []string) error
	SendText(context.Context, string, string) error
}

// Gateway is a Herdr gateway whose Binding and BoundInput are verified by the
// Bridge. Every other method is the embedded gateway's.
type Gateway struct {
	*herdr.Gateway
	api       herdrAPI
	root      string
	startTime func(pid uint32) (StartTime, error)
}

// New reads native session metadata from claudeRoot/sessions.
func New(g *herdr.Gateway, claudeRoot string) *Gateway {
	return &Gateway{Gateway: g, api: g, root: claudeRoot, startTime: processStartTime}
}

// Binding inspects the pane's foreground job. The binding names the foreground
// job leader only if it is Claude, its sessions/<pid>.json belongs to this
// process incarnation, and neither the process nor the foreground job changed
// while the file was read. A Herdr error is returned as is; every other
// failure wraps ErrUnverified.
func (g *Gateway) Binding(ctx context.Context, paneID string) (herdr.Binding, error) {
	if paneID == "" {
		return herdr.Binding{}, herdr.ErrEmptyTarget
	}
	agent, err := g.api.AgentInfo(ctx, paneID)
	if err != nil {
		return herdr.Binding{}, err
	}
	first, err := g.api.ProcessInfo(ctx, paneID)
	if err != nil {
		return herdr.Binding{}, err
	}
	leader, ok := claudeLeader(first)
	if !ok {
		return herdr.Binding{}, fmt.Errorf("%w: foreground job leader is not Claude", ErrUnverified)
	}
	started, err := g.startTime(leader)
	if err != nil {
		return herdr.Binding{}, fmt.Errorf("%w: %w", ErrUnverified, err)
	}
	native, err := readMetadata(g.root, leader, started)
	if err != nil {
		return herdr.Binding{}, fmt.Errorf("%w: %w", ErrUnverified, err)
	}
	if again, err := g.startTime(leader); err != nil || again != started {
		return herdr.Binding{}, fmt.Errorf("%w: process changed during inspection", ErrUnverified)
	}
	second, err := g.api.ProcessInfo(ctx, paneID)
	if err != nil {
		return herdr.Binding{}, err
	}
	if second.ShellPID == nil || *second.ShellPID != *first.ShellPID || second.ForegroundProcessGroupID == nil || *second.ForegroundProcessGroupID != leader {
		return herdr.Binding{}, fmt.Errorf("%w: foreground job changed during inspection", ErrUnverified)
	}
	return herdr.Binding{
		Token:           token(agent.TerminalID, leader, started, native.SessionID),
		TerminalID:      agent.TerminalID,
		NativeSessionID: native.SessionID,
		ProcessID:       leader,
		Agent:           "claude",
		CWD:             native.CWD,
	}, nil
}

// claudeLeader returns the PID of the foreground job leader when it is
// Claude. Like Herdr's own detection, argv0 decides and name is the fallback:
// Claude's process name is its version string.
func claudeLeader(info herdr.ProcessInfo) (uint32, bool) {
	if info.ShellPID == nil || info.ForegroundProcessGroupID == nil {
		return 0, false
	}
	leader := *info.ForegroundProcessGroupID
	for _, p := range info.ForegroundProcesses {
		if p.PID != leader {
			continue
		}
		name := p.Argv0
		if name == "" {
			name = p.Name
		}
		return leader, strings.EqualFold(path.Base(strings.TrimSpace(name)), "claude")
	}
	return 0, false
}

type metadata struct {
	PID       uint32 `json:"pid"`
	SessionID string `json:"sessionId"`
	CWD       string `json:"cwd"`
	ProcStart string `json:"procStart"`
	Kind      string `json:"kind"`
}

func readMetadata(root string, pid uint32, started StartTime) (metadata, error) {
	name := filepath.Join(root, "sessions", strconv.FormatUint(uint64(pid), 10)+".json")
	// Opening a FIFO planted at the path would block, so only a regular file
	// is opened, and it must still be that file once open.
	info, err := os.Stat(name)
	if err != nil {
		return metadata{}, err
	}
	if !info.Mode().IsRegular() {
		return metadata{}, errors.New("session metadata is not a regular file")
	}
	f, err := os.Open(name)
	if err != nil {
		return metadata{}, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return metadata{}, err
	}
	if !os.SameFile(info, opened) {
		return metadata{}, errors.New("session metadata replaced while opening")
	}
	if opened.ModTime().UnixMicro() < started.micros() {
		return metadata{}, errors.New("session metadata belongs to an earlier process with this PID")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxMetadataBytes+1))
	if err != nil {
		return metadata{}, err
	}
	if len(data) > maxMetadataBytes {
		return metadata{}, errors.New("session metadata too large")
	}
	var m metadata
	if err := json.Unmarshal(data, &m); err != nil {
		return metadata{}, fmt.Errorf("decode session metadata: %w", err)
	}
	if m.PID != pid || m.ProcStart != procStart(started) || m.Kind != "interactive" || m.SessionID == "" || m.CWD == "" {
		return metadata{}, errors.New("session metadata does not match the process")
	}
	return m, nil
}

func procStart(started StartTime) string {
	return time.Unix(started.Sec, 0).UTC().Format(procStartLayout)
}

// token changes when the terminal, the process incarnation or the native
// session changes. Clients treat it as opaque.
func token(terminalID string, pid uint32, started StartTime, sessionID string) string {
	parts := []string{terminalID, strconv.FormatUint(uint64(pid), 10), strconv.FormatInt(started.Sec, 10), strconv.FormatInt(started.Usec, 10), sessionID}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

// BoundInput sends input only if the pane still runs the session that token
// names. A *Rejected error or herdr.ErrUnsupported means nothing reached the
// PTY; an error wrapping ErrDeliveryUnknown means some input may have. Nothing
// is retried.
func (g *Gateway) BoundInput(ctx context.Context, paneID, binding, kind, text string) error {
	if kind != "prompt" && kind != "interrupt" && kind != "terminal_input" {
		return &Rejected{CodeInputRejected, fmt.Errorf("unsupported bound input type %q", kind)}
	}
	current, err := g.Binding(ctx, paneID)
	if errors.Is(err, herdr.ErrUnsupported) {
		return err
	}
	if err != nil {
		return &Rejected{CodeSessionEnded, err}
	}
	if binding == "" || current.Token != binding {
		return &Rejected{CodeBindingMismatch, errors.New("native session changed")}
	}
	switch kind {
	case "prompt":
		if err := validatePrompt(text); err != nil {
			return &Rejected{CodeInvalidPrompt, err}
		}
		if err := g.api.SendInput(ctx, paneID, text, nil); err != nil {
			return firstSendError(err)
		}
		if err := g.submit(ctx, paneID, binding); err != nil {
			return fmt.Errorf("%w: prompt text sent, Enter withheld or uncertain: %w", ErrDeliveryUnknown, err)
		}
		return nil
	case "interrupt":
		return firstSendError(g.api.SendKeys(ctx, paneID, []string{"ctrl+c"}))
	default:
		if text == "" || len(text) > maxInputBytes {
			return &Rejected{CodeInvalidInput, errors.New("raw input empty or too large")}
		}
		return firstSendError(g.api.SendText(ctx, paneID, text))
	}
}

// submit sends Enter after the prompt text only if the same session is still
// in the foreground once the text has settled.
func (g *Gateway) submit(ctx context.Context, paneID, binding string) error {
	timer := time.NewTimer(promptSettle)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
	}
	current, err := g.Binding(ctx, paneID)
	if err != nil {
		return err
	}
	if current.Token != binding {
		return errors.New("native session changed after the prompt text")
	}
	return g.api.SendKeys(ctx, paneID, []string{"enter"})
}

func validatePrompt(text string) error {
	if text == "" || len(text) > maxInputBytes || !utf8.ValidString(text) {
		return errors.New("prompt is empty, too large or not UTF-8")
	}
	for _, r := range text {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return errors.New("prompt contains control characters")
		}
	}
	return nil
}

// firstSendError classifies the failure of the first and only write request
// sent so far. Herdr answers an API error before queueing anything, so that
// is a definite non-delivery; a transport or response failure is not.
func firstSendError(err error) error {
	var api *herdr.APIError
	switch {
	case err == nil:
		return nil
	case errors.Is(err, herdr.ErrUnsupported):
		return err
	case errors.As(err, &api) && api.Code == "pane_not_found":
		return &Rejected{CodeSessionEnded, err}
	case errors.As(err, &api):
		return &Rejected{CodeInputRejected, err}
	default:
		return fmt.Errorf("%w: %w", ErrDeliveryUnknown, err)
	}
}
