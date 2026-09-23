// doctor only observes the existing Herdr server; it never starts one.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"herdr-remote/internal/herdr"
	"io"
	"log/slog"
	"os"
	"os/signal"
)

type reader interface {
	Snapshot(context.Context) (herdr.Snapshot, error)
	ProcessInfo(context.Context, string) (herdr.ProcessInfo, error)
	TerminalSnapshot(context.Context, string) (herdr.TerminalSnapshot, error)
	Binding(context.Context, string) (herdr.Binding, error)
}

type agentReport struct {
	PaneID                   string `json:"pane_id"`
	Agent                    string `json:"agent"`
	Status                   string `json:"herdr_status"`
	NativeAssociation        bool   `json:"native_association"`
	ProcessCount             int    `json:"foreground_process_count"`
	ProcessError             string `json:"process_error,omitempty"`
	VisibleSnapshotAvailable bool   `json:"visible_snapshot_available"`
	TerminalReadError        string `json:"terminal_read_error,omitempty"`
	VerifiedBinding          bool   `json:"verified_binding"`
}

type report struct {
	Version               string        `json:"herdr_version"`
	Protocol              int           `json:"herdr_protocol"`
	WriteEnabled          bool          `json:"write_enabled"`
	TerminalMirrorEnabled bool          `json:"terminal_mirror_enabled"`
	Blockers              []string      `json:"blockers"`
	Agents                []agentReport `json:"agents"`
}

func diagnose(ctx context.Context, gateway reader, out io.Writer) error {
	s, err := gateway.Snapshot(ctx)
	if err != nil {
		return fmt.Errorf("read Herdr snapshot: %w", err)
	}
	r := report{Version: s.Version, Protocol: s.Protocol, Agents: []agentReport{}, Blockers: []string{}}
	for _, a := range s.Agents {
		item := agentReport{PaneID: a.PaneID, Agent: a.Agent, Status: a.Status}
		item.NativeAssociation = a.Session != nil && a.Session.Value != "" && a.Session.Agent == a.Agent && a.Session.Kind == "id"
		p, err := gateway.ProcessInfo(ctx, a.PaneID)
		if err != nil {
			item.ProcessError = err.Error()
		} else {
			item.ProcessCount = len(p.ForegroundProcesses)
		}
		_, err = gateway.TerminalSnapshot(ctx, a.PaneID)
		if err != nil {
			item.TerminalReadError = err.Error()
		} else {
			item.VisibleSnapshotAvailable = true
		}
		binding, bindingErr := gateway.Binding(ctx, a.PaneID)
		if bindingErr == nil && binding.Token != "" && binding.NativeSessionID != "" && binding.TerminalID == a.TerminalID && binding.Agent == a.Agent {
			for _, process := range p.ForegroundProcesses {
				if process.PID == binding.ProcessID {
					item.VerifiedBinding = true
					break
				}
			}
		}
		if item.VerifiedBinding {
			r.WriteEnabled = true
			if item.VisibleSnapshotAvailable {
				r.TerminalMirrorEnabled = true
			}
		}
		r.Agents = append(r.Agents, item)
	}
	if !r.WriteEnabled {
		r.Blockers = append(r.Blockers, "No verified native runtime binding; conditional input remains unavailable.")
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

func main() {
	socket := flag.String("socket", os.Getenv("HERDR_SOCKET_PATH"), "existing Herdr public API socket (or HERDR_SOCKET_PATH)")
	flag.Parse()
	if *socket == "" {
		slog.Error("explicit -socket or HERDR_SOCKET_PATH is required")
		os.Exit(2)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if err := diagnose(ctx, herdr.NewGateway(*socket), os.Stdout); err != nil {
		slog.Error("Herdr inspection failed", "error", err)
		os.Exit(1)
	}
}
