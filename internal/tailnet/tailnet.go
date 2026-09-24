// Package tailnet reads the local Tailscale state through the CLI's --json
// output. It only runs read-only commands (`status`, `serve status`) and never
// configures Serve, certificates, or Funnel.
package tailnet

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// Runner executes the Tailscale CLI with args and returns its stdout.
type Runner func(ctx context.Context, args ...string) ([]byte, error)

// Flag values shared by cmd/bridge and cmd/doctor.
const (
	Auto = "auto"
	Off  = "off"
)

// Sources reported for the resolved host and login.
const (
	SourceAuto        = "auto"
	SourceExplicit    = "explicit"
	SourceOff         = "off"
	SourceUnavailable = "unavailable"
)

const taggedLogin = "tagged-devices"

// WellKnownPaths are tried after PATH lookup. launchd services only get
// /usr/bin:/bin:/usr/sbin:/sbin, and /usr/local/bin/tailscale on macOS is a
// shell wrapper around the app binary.
var WellKnownPaths = []string{
	"/Applications/Tailscale.app/Contents/MacOS/Tailscale",
	"/Applications/Tailscale.app/Contents/MacOS/tailscale",
	"/opt/homebrew/bin/tailscale",
	"/usr/local/bin/tailscale",
	"/usr/bin/tailscale",
}

// FindCLI returns the Tailscale CLI path. A non-empty explicit value wins.
func FindCLI(explicit string) (string, error) {
	return findCLI(explicit, exec.LookPath, WellKnownPaths)
}

func findCLI(explicit string, lookPath func(string) (string, error), candidates []string) (string, error) {
	if explicit != "" {
		if strings.ContainsRune(explicit, os.PathSeparator) {
			if !executable(explicit) {
				return "", fmt.Errorf("tailscale CLI %q is not an executable file", explicit)
			}
			return explicit, nil
		}
		return lookPath(explicit)
	}
	if path, err := lookPath("tailscale"); err == nil {
		return path, nil
	}
	for _, path := range candidates {
		if executable(path) {
			return path, nil
		}
	}
	return "", errors.New("tailscale CLI not found in PATH or well-known locations")
}

func executable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

// ExecRunner runs the CLI at bin.
func ExecRunner(bin string) Runner {
	return func(ctx context.Context, args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, bin, args...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			if msg := strings.TrimSpace(stderr.String()); msg != "" {
				return nil, fmt.Errorf("%s %s: %w: %s", bin, strings.Join(args, " "), err, msg)
			}
			return nil, fmt.Errorf("%s %s: %w", bin, strings.Join(args, " "), err)
		}
		return out, nil
	}
}

// Status is the subset of `tailscale status --json` used here.
type Status struct {
	BackendState string
	CertDomains  []string
	Self         *struct {
		DNSName string
		UserID  int64
		Tags    []string
	}
	User map[string]struct {
		LoginName string
	}
}

// ReadStatus runs `tailscale status --json`.
func ReadStatus(ctx context.Context, run Runner) (Status, error) {
	var s Status
	out, err := run(ctx, "status", "--json")
	if err != nil {
		return s, err
	}
	if err = json.Unmarshal(out, &s); err != nil {
		return s, fmt.Errorf("parse tailscale status: %w", err)
	}
	return s, nil
}

// Host returns the node's MagicDNS name without the trailing dot.
func (s Status) Host() (string, error) {
	if s.BackendState != "Running" {
		return "", fmt.Errorf("tailscale backend is %q, not Running", s.BackendState)
	}
	if s.Self == nil || strings.TrimSuffix(s.Self.DNSName, ".") == "" {
		return "", errors.New("tailscale status has no Self.DNSName (MagicDNS disabled?)")
	}
	return strings.TrimSuffix(s.Self.DNSName, "."), nil
}

// Login returns the login of the user owning this node. Tagged nodes have no
// owner user, so they return an error.
func (s Status) Login() (string, error) {
	if s.BackendState != "Running" {
		return "", fmt.Errorf("tailscale backend is %q, not Running", s.BackendState)
	}
	if s.Self == nil {
		return "", errors.New("tailscale status has no Self")
	}
	if len(s.Self.Tags) > 0 {
		return "", errors.New("node is tagged and has no owner login")
	}
	login := s.User[strconv.FormatInt(s.Self.UserID, 10)].LoginName
	if login == taggedLogin {
		return "", errors.New("node is tagged and has no owner login")
	}
	if login == "" {
		return "", errors.New("tailscale status has no login for Self.UserID")
	}
	return login, nil
}

// HasCertDomain reports whether HTTPS certificates are available for host.
func (s Status) HasCertDomain(host string) bool {
	for _, domain := range s.CertDomains {
		if strings.EqualFold(strings.TrimSuffix(domain, "."), strings.TrimSuffix(host, ".")) {
			return true
		}
	}
	return false
}

// Identity is the tailnet host and owner login Bridge enforces.
type Identity struct {
	Host        string
	HostSource  string
	Login       string
	LoginSource string
	// Warnings explains every auto value that could not be resolved.
	Warnings []string
}

// Resolve applies the -tailnet-host/-tailnet-login flags. run may be nil when
// the CLI is unavailable; cliErr then explains why. Resolution never fails:
// unresolved auto values fall back to empty, which Bridge treats as
// localhost-only (no host) or reject-all tailnet requests (host without login).
func Resolve(ctx context.Context, hostFlag, loginFlag string, run Runner, cliErr error) Identity {
	if hostFlag == Off {
		return Identity{HostSource: SourceOff, LoginSource: SourceOff}
	}
	id := Identity{Host: strings.TrimSuffix(hostFlag, "."), HostSource: SourceExplicit, Login: loginFlag, LoginSource: SourceExplicit}
	if hostFlag != Auto && loginFlag != Auto {
		return id
	}
	var status Status
	var statusErr error
	if run == nil {
		statusErr = cliErr
		if statusErr == nil {
			statusErr = errors.New("tailscale CLI unavailable")
		}
	} else {
		status, statusErr = ReadStatus(ctx, run)
	}
	if hostFlag == Auto {
		id.Host, id.HostSource = "", SourceUnavailable
		if statusErr != nil {
			id.Warnings = append(id.Warnings, "tailnet host unresolved: "+statusErr.Error())
		} else if host, err := status.Host(); err != nil {
			id.Warnings = append(id.Warnings, "tailnet host unresolved: "+err.Error())
		} else {
			id.Host, id.HostSource = host, SourceAuto
		}
	}
	if loginFlag == Auto {
		id.Login, id.LoginSource = "", SourceUnavailable
		if id.Host == "" {
			// Without a host every tailnet request is rejected; the login is moot.
		} else if statusErr != nil {
			id.Warnings = append(id.Warnings, "tailnet owner login unresolved: "+statusErr.Error())
		} else if login, err := status.Login(); err != nil {
			id.Warnings = append(id.Warnings, "tailnet owner login unresolved, rejecting all tailnet requests: "+err.Error())
		} else {
			id.Login, id.LoginSource = login, SourceAuto
		}
	}
	return id
}

// WithOrigin returns origins plus https://host, without duplicates or empty
// entries. An empty host leaves origins unchanged.
func WithOrigin(origins []string, host string) []string {
	out := make([]string, 0, len(origins)+1)
	seen := map[string]bool{}
	add := func(origin string) {
		origin = strings.TrimSpace(origin)
		if origin != "" && !seen[origin] {
			seen[origin] = true
			out = append(out, origin)
		}
	}
	for _, origin := range origins {
		add(origin)
	}
	if host != "" {
		add("https://" + host)
	}
	return out
}

// ServeConfig is the subset of Tailscale's ipn.ServeConfig used here. Only
// background (`--bg`) config lives in Web; foreground sessions are ignored
// because they do not outlive the CLI process.
type ServeConfig struct {
	Web map[string]*struct {
		Handlers map[string]*struct {
			Proxy string
		}
	}
	AllowFunnel map[string]bool
}

// ReadServeConfig runs `tailscale serve status --json`.
func ReadServeConfig(ctx context.Context, run Runner) (ServeConfig, error) {
	var c ServeConfig
	out, err := run(ctx, "serve", "status", "--json")
	if err != nil {
		return c, err
	}
	if err = json.Unmarshal(out, &c); err != nil {
		return c, fmt.Errorf("parse tailscale serve status: %w", err)
	}
	return c, nil
}

// ProxiesTo reports whether https://host/ is proxied to the Bridge listen
// address (host:port, e.g. 127.0.0.1:8787).
func (c ServeConfig) ProxiesTo(host, listen string) bool {
	web := c.Web[host+":443"]
	if web == nil {
		return false
	}
	handler := web.Handlers["/"]
	return handler != nil && proxyTargets(handler.Proxy, listen)
}

// FunnelEnabled reports whether Funnel is allowed on any port of host.
func (c ServeConfig) FunnelEnabled(host string) bool {
	for hostPort, allowed := range c.AllowFunnel {
		if h, _, err := net.SplitHostPort(hostPort); allowed && err == nil && strings.EqualFold(h, host) {
			return true
		}
	}
	return false
}

// proxyTargets accepts the forms Tailscale stores or accepts for a proxy
// target: a bare port, host:port, or an http URL with an optional "/" path.
func proxyTargets(proxy, listen string) bool {
	listenHost, listenPort, err := net.SplitHostPort(listen)
	if err != nil || proxy == "" {
		return false
	}
	if _, err := strconv.ParseUint(proxy, 10, 16); err == nil {
		proxy = "127.0.0.1:" + proxy
	}
	if !strings.Contains(proxy, "://") {
		proxy = "http://" + proxy
	}
	u, err := url.Parse(proxy)
	if err != nil || u.Scheme != "http" || u.Port() != listenPort || u.Path != "" && u.Path != "/" || u.RawQuery != "" {
		return false
	}
	return loopbackMatch(u.Hostname(), listenHost)
}

func loopbackMatch(target, listen string) bool {
	if target == listen {
		return true
	}
	// Go resolves "localhost" to either loopback family.
	return target == "localhost" && (listen == "127.0.0.1" || listen == "::1") ||
		listen == "localhost" && (target == "127.0.0.1" || target == "::1")
}
