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
	"time"
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

const (
	// execWaitDelay bounds how long a canceled run waits for its I/O pipes.
	// The macOS /usr/local/bin/tailscale wrapper is a shell script that runs
	// the app binary without exec, so a context kill only hits the shell and
	// the grandchild keeps stdout open.
	execWaitDelay = time.Second
	// maxOutputBytes caps stdout and stderr of a single CLI run.
	maxOutputBytes = 4 << 20
)

// ExecRunner runs the CLI at bin. A canceled context returns within
// execWaitDelay even if a grandchild still holds the output pipes.
func ExecRunner(bin string) Runner {
	return execRunner(bin, execWaitDelay, maxOutputBytes)
}

func execRunner(bin string, waitDelay time.Duration, limit int) Runner {
	return func(ctx context.Context, args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, bin, args...)
		cmd.WaitDelay = waitDelay
		stdout := &cappedBuffer{limit: limit}
		stderr := &cappedBuffer{limit: limit}
		cmd.Stdout, cmd.Stderr = stdout, stderr
		err := cmd.Run()
		if err == nil && stdout.exceeded {
			err = fmt.Errorf("stdout exceeds %d bytes", limit)
		}
		if err != nil {
			if msg := strings.TrimSpace(stderr.String()); msg != "" {
				return nil, fmt.Errorf("%s %s: %w: %s", bin, strings.Join(args, " "), err, msg)
			}
			return nil, fmt.Errorf("%s %s: %w", bin, strings.Join(args, " "), err)
		}
		return stdout.Bytes(), nil
	}
}

// cappedBuffer keeps at most limit bytes and discards the rest, so the child
// never blocks on a full pipe; exceeded marks the output as unusable. It does
// not embed bytes.Buffer: a promoted ReadFrom would let io.Copy bypass Write.
type cappedBuffer struct {
	buf      bytes.Buffer
	limit    int
	exceeded bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if room := b.limit - b.buf.Len(); len(p) > room {
		b.exceeded = true
		if room > 0 {
			b.buf.Write(p[:room])
		}
		return len(p), nil
	}
	return b.buf.Write(p)
}

func (b *cappedBuffer) Bytes() []byte  { return b.buf.Bytes() }
func (b *cappedBuffer) String() string { return b.buf.String() }

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

// DropUnverifiedOrigins splits origins into those kept and the tailnet HTTPS
// Origins removed because the owner login (or, for an auto host, the host)
// could not be resolved. Keeping them would make Origin validation stop
// Bridge, while a tailnet setup failure must only leave tailnet requests
// rejected. Malformed, wildcard, loopback, and mismatched-host Origins are
// kept so validation still reports them.
func (id Identity) DropUnverifiedOrigins(origins []string) (kept, dropped []string) {
	for _, origin := range origins {
		if id.Login == "" && id.unverifiedTailnetOrigin(strings.TrimSpace(origin)) {
			dropped = append(dropped, origin)
		} else {
			kept = append(kept, origin)
		}
	}
	return kept, dropped
}

func (id Identity) unverifiedTailnetOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return false
	}
	name := u.Hostname()
	if name == "localhost" || name == "127.0.0.1" || strings.Contains(name, "*") {
		return false
	}
	if id.Host != "" {
		return strings.EqualFold(name, id.Host)
	}
	return id.HostSource == SourceUnavailable
}

// ServeConfig is the subset of Tailscale's ipn.ServeConfig used here. The
// top-level Web and AllowFunnel hold background (`--bg`) config. A foreground
// `tailscale serve`/`funnel` run stores its own config in Foreground, keyed
// by CLI session ID, for as long as that CLI process lives.
type ServeConfig struct {
	Web map[string]*struct {
		Handlers map[string]*struct {
			Proxy string
		}
	}
	AllowFunnel map[string]bool
	Foreground  map[string]*ServeConfig
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

// ProxiesTo reports whether background config proxies https://host/ to the
// Bridge listen address (host:port, e.g. 127.0.0.1:8787). Foreground configs
// are ignored: they end with the CLI session, so they are not a durable setup.
func (c ServeConfig) ProxiesTo(host, listen string) bool {
	web := c.Web[host+":443"]
	if web == nil {
		return false
	}
	handler := web.Handlers["/"]
	return handler != nil && proxyTargets(handler.Proxy, listen)
}

// FunnelEnabled reports whether Funnel is allowed on any port of host, in
// background or any foreground config, like upstream ServeConfig.IsFunnelOn.
func (c ServeConfig) FunnelEnabled(host string) bool {
	for hostPort, allowed := range c.AllowFunnel {
		if h, _, err := net.SplitHostPort(hostPort); allowed && err == nil && strings.EqualFold(h, host) {
			return true
		}
	}
	for _, fg := range c.Foreground {
		if fg != nil && fg.FunnelEnabled(host) {
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
