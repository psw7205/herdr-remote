// doctor only observes the existing Herdr server and Tailscale state; it never
// starts Herdr or changes Tailscale Serve, certificates, or Funnel.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/psw7205/herdr-remote/internal/herdr"
	"github.com/psw7205/herdr-remote/internal/tailnet"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strings"
	"time"
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
	ConditionalInput      string        `json:"conditional_input"`
	Blockers              []string      `json:"blockers"`
	Agents                []agentReport `json:"agents"`
	Tailnet               tailnetReport `json:"tailnet"`
}

// tailnetReport describes remote-access setup. Missing setup is an issue, not
// a blocker, because localhost-only use is supported.
type tailnetReport struct {
	CLIAvailable      bool     `json:"cli_available"`
	BackendState      string   `json:"backend_state"`
	Host              string   `json:"host"`
	Login             string   `json:"login"`
	HTTPSCertificates bool     `json:"https_certificates"`
	ServeProxy        bool     `json:"serve_proxy"`
	Funnel            bool     `json:"funnel"`
	TCPForward        bool     `json:"tcp_forward"`
	TCPForwardPorts   []string `json:"tcp_forward_ports,omitempty"`
	Issues            []string `json:"issues"`
}

// inspectTailnet runs only read-only `status --json` and `serve status --json`.
// run is nil when the CLI is unavailable.
func inspectTailnet(ctx context.Context, run tailnet.Runner, cliErr error, listen string) tailnetReport {
	r := tailnetReport{Issues: []string{}}
	if run == nil {
		r.Issues = append(r.Issues, fmt.Sprintf("Tailscale CLI unavailable (%v); Bridge stays localhost-only. Install Tailscale or pass -tailscale-bin.", cliErr))
		return r
	}
	r.CLIAvailable = true
	status, err := tailnet.ReadStatus(ctx, run)
	if err != nil {
		r.Issues = append(r.Issues, fmt.Sprintf("tailscale status failed: %v", err))
		return r
	}
	r.BackendState = status.BackendState
	host, err := status.Host()
	if err != nil {
		if status.BackendState != "Running" {
			r.Issues = append(r.Issues, fmt.Sprintf("Tailscale backend is %q; connect this host with `tailscale up`.", status.BackendState))
		} else {
			r.Issues = append(r.Issues, fmt.Sprintf("Tailnet host unavailable (%v); enable MagicDNS in the Tailscale admin console.", err))
		}
		// Funnel and raw TCP forwards are still read: they expose Bridge
		// whether or not the HTTPS proxy can be set up.
		if serve, err := tailnet.ReadServeConfig(ctx, run); err != nil {
			r.Issues = append(r.Issues, fmt.Sprintf("tailscale serve status failed: %v", err))
		} else {
			r.inspectExposure(serve, "", listen)
		}
		return r
	}
	r.Host = host
	if login, err := status.Login(); err != nil {
		r.Issues = append(r.Issues, fmt.Sprintf("No owner login (%v); Bridge rejects every tailnet request unless started with an explicit -tailnet-login.", err))
	} else {
		r.Login = login
	}
	r.HTTPSCertificates = status.HasCertDomain(host)
	if !r.HTTPSCertificates {
		r.Issues = append(r.Issues, fmt.Sprintf("HTTPS certificate unavailable for %s; enable HTTPS Certificates on the DNS page of the Tailscale admin console.", host))
	}
	serve, err := tailnet.ReadServeConfig(ctx, run)
	if err != nil {
		r.Issues = append(r.Issues, fmt.Sprintf("tailscale serve status failed: %v", err))
		return r
	}
	r.ServeProxy = serve.ProxiesTo(host, listen)
	r.inspectExposure(serve, host, listen)
	if !r.ServeProxy {
		port := listen
		if _, p, err := net.SplitHostPort(listen); err == nil {
			port = p
		}
		r.Issues = append(r.Issues, fmt.Sprintf("Tailscale Serve does not proxy https://%s/ to http://%s; run `tailscale serve --bg %s`.", host, listen, port))
	}
	return r
}

// inspectExposure records Funnel on host (any host when "") and raw TCP
// forwards to Bridge.
func (r *tailnetReport) inspectExposure(serve tailnet.ServeConfig, host, listen string) {
	r.Funnel = serve.FunnelEnabled(host)
	if ports := serve.TCPForwardsTo(listen); len(ports) > 0 {
		r.TCPForward, r.TCPForwardPorts = true, ports
	}
}

func diagnose(ctx context.Context, gateway reader, tn tailnetReport, out io.Writer) error {
	s, err := gateway.Snapshot(ctx)
	if err != nil {
		return fmt.Errorf("read Herdr snapshot: %w", err)
	}
	r := report{Version: s.Version, Protocol: s.Protocol, ConditionalInput: herdr.ConditionalInputUnknown, Agents: []agentReport{}, Blockers: []string{}, Tailnet: tn}
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
		r.ConditionalInput = herdr.ObserveConditionalInput(r.ConditionalInput, bindingErr)
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
	if r.ConditionalInput == herdr.ConditionalInputUnsupported {
		r.Blockers = append(r.Blockers, "Herdr server lacks agent.binding/agent.bound_input (likely a stock build); conditional input remains unavailable. See docs/herdr-patch.md.")
	} else if !r.WriteEnabled {
		r.Blockers = append(r.Blockers, "No verified native runtime binding; conditional input remains unavailable.")
	}
	if tn.Funnel {
		target := tn.Host
		if target == "" {
			target = "this node"
		}
		r.Blockers = append(r.Blockers, fmt.Sprintf("Tailscale Funnel is enabled for %s and would expose Bridge to the public internet; clear it with `tailscale funnel reset` (this resets all Serve config), then re-create the tailnet-only proxy with `tailscale serve --bg <Bridge port>`.", target))
	}
	if tn.TCPForward {
		r.Blockers = append(r.Blockers, fmt.Sprintf("Tailscale Serve forwards raw TCP port(s) %s to Bridge; tailnet peers would reach Bridge from loopback without Tailscale identity headers. Remove each with `tailscale serve --tcp=<port> off` (or `--tls-terminated-tcp=<port> off`; for a `svc:<name>:<port>` entry add `--service=svc:<name>`), stop any foreground `tailscale serve` process, and expose Bridge only through the HTTPS proxy `tailscale serve --bg <Bridge port>`.", strings.Join(tn.TCPForwardPorts, ", ")))
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

func main() {
	socket := flag.String("socket", os.Getenv("HERDR_SOCKET_PATH"), "existing Herdr public API socket (or HERDR_SOCKET_PATH)")
	tailscaleBin := flag.String("tailscale-bin", "", "Tailscale CLI path (default: PATH, then well-known install locations)")
	bridgeListen := flag.String("bridge-listen", "127.0.0.1:8787", "Bridge listen address that Tailscale Serve should proxy to")
	flag.Parse()
	if *socket == "" {
		slog.Error("explicit -socket or HERDR_SOCKET_PATH is required")
		os.Exit(2)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	var run tailnet.Runner
	path, cliErr := tailnet.FindCLI(*tailscaleBin)
	if cliErr == nil {
		run = tailnet.ExecRunner(path)
	}
	tailnetCtx, cancelTailnet := context.WithTimeout(ctx, 5*time.Second)
	tn := inspectTailnet(tailnetCtx, run, cliErr, *bridgeListen)
	cancelTailnet()
	if err := diagnose(ctx, herdr.NewGateway(*socket), tn, os.Stdout); err != nil {
		slog.Error("Herdr inspection failed", "error", err)
		os.Exit(1)
	}
}
