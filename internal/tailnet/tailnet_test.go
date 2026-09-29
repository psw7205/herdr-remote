package tailnet

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

// statusRunning is an anonymized `tailscale status --json` shape: field names
// and value types match a live 1.102 host, every value is synthetic.
const statusRunning = `{
  "BackendState": "Running",
  "CertDomains": ["node.example-tailnet.ts.net"],
  "Self": {"ID": "n1", "DNSName": "node.example-tailnet.ts.net.", "UserID": 1234567890123456, "TailscaleIPs": ["100.64.0.1"]},
  "User": {
    "1234567890123456": {"ID": 1234567890123456, "LoginName": "owner@example.com", "DisplayName": "Owner"},
    "42": {"ID": 42, "LoginName": "peer@example.com"}
  }
}`

// statusNoCerts mirrors a tailnet without HTTPS certificates: CertDomains is null.
const statusNoCerts = `{
  "BackendState": "Running",
  "CertDomains": null,
  "Self": {"DNSName": "node.example-tailnet.ts.net.", "UserID": 7},
  "User": {"7": {"ID": 7, "LoginName": "owner@example.com"}}
}`

// statusTagged is a tagged node: Tailscale reports Tags on Self and the
// pseudo-user "tagged-devices" as its owner.
const statusTagged = `{
  "BackendState": "Running",
  "Self": {"DNSName": "server.example-tailnet.ts.net.", "UserID": 9, "Tags": ["tag:server"]},
  "User": {"9": {"ID": 9, "LoginName": "tagged-devices"}}
}`

const statusStopped = `{"BackendState": "Stopped", "Self": {"DNSName": "", "UserID": 7}}`

func fixedRunner(outputs map[string]string, calls *[]string) Runner {
	return func(_ context.Context, args ...string) ([]byte, error) {
		key := strings.Join(args, " ")
		if calls != nil {
			*calls = append(*calls, key)
		}
		out, ok := outputs[key]
		if !ok {
			return nil, errors.New("unexpected tailscale call: " + key)
		}
		return []byte(out), nil
	}
}

func TestResolveAutoUsesSelfDNSNameAndOwnerLogin(t *testing.T) {
	var calls []string
	id := Resolve(context.Background(), Auto, Auto, fixedRunner(map[string]string{"status --json": statusRunning}, &calls), nil)
	if id.Host != "node.example-tailnet.ts.net" || id.HostSource != SourceAuto {
		t.Fatalf("host: %+v", id)
	}
	if id.Login != "owner@example.com" || id.LoginSource != SourceAuto || len(id.Warnings) != 0 {
		t.Fatalf("login: %+v", id)
	}
	if len(calls) != 1 {
		t.Fatalf("status must be read once, got %v", calls)
	}
}

func TestResolveTaggedNodeKeepsHostWithoutLogin(t *testing.T) {
	id := Resolve(context.Background(), Auto, Auto, fixedRunner(map[string]string{"status --json": statusTagged}, nil), nil)
	if id.Host != "server.example-tailnet.ts.net" || id.Login != "" || id.LoginSource != SourceUnavailable || len(id.Warnings) != 1 {
		t.Fatalf("tagged node must fail closed: %+v", id)
	}
	// Tags alone are enough even if the user map looks like a person.
	tagsOnly := strings.Replace(statusTagged, "tagged-devices", "someone@example.com", 1)
	id = Resolve(context.Background(), Auto, Auto, fixedRunner(map[string]string{"status --json": tagsOnly}, nil), nil)
	if id.Login != "" {
		t.Fatalf("tagged node exposed a login: %+v", id)
	}
}

func TestResolveFallsBackToLocalhostOnly(t *testing.T) {
	failing := func(context.Context, ...string) ([]byte, error) { return nil, errors.New("exit status 1") }
	cases := map[string]Identity{
		"missing CLI": Resolve(context.Background(), Auto, Auto, nil, errors.New("tailscale CLI not found")),
		"CLI error":   Resolve(context.Background(), Auto, Auto, failing, nil),
		"stopped":     Resolve(context.Background(), Auto, Auto, fixedRunner(map[string]string{"status --json": statusStopped}, nil), nil),
		"bad JSON":    Resolve(context.Background(), Auto, Auto, fixedRunner(map[string]string{"status --json": "not json"}, nil), nil),
	}
	for name, id := range cases {
		if id.Host != "" || id.Login != "" || id.HostSource != SourceUnavailable || id.LoginSource != SourceUnavailable || len(id.Warnings) == 0 {
			t.Errorf("%s: %+v", name, id)
		}
	}
}

func TestResolveExplicitAndOff(t *testing.T) {
	var calls []string
	run := fixedRunner(map[string]string{"status --json": statusRunning}, &calls)
	id := Resolve(context.Background(), "other.example-tailnet.ts.net.", "me@example.com", run, nil)
	if id.Host != "other.example-tailnet.ts.net" || id.Login != "me@example.com" || id.HostSource != SourceExplicit || id.LoginSource != SourceExplicit {
		t.Fatalf("explicit: %+v", id)
	}
	id = Resolve(context.Background(), Off, "me@example.com", run, nil)
	if !reflect.DeepEqual(id, Identity{HostSource: SourceOff, LoginSource: SourceOff}) {
		t.Fatalf("off: %+v", id)
	}
	if len(calls) != 0 {
		t.Fatalf("explicit/off must not call the CLI: %v", calls)
	}
	id = Resolve(context.Background(), "other.example-tailnet.ts.net", Auto, run, nil)
	if id.HostSource != SourceExplicit || id.Login != "owner@example.com" || id.LoginSource != SourceAuto {
		t.Fatalf("explicit host with auto login: %+v", id)
	}
	id = Resolve(context.Background(), Auto, "me@example.com", run, nil)
	if id.Host != "node.example-tailnet.ts.net" || id.Login != "me@example.com" || id.LoginSource != SourceExplicit {
		t.Fatalf("auto host with explicit login: %+v", id)
	}
}

func TestStatusCertDomains(t *testing.T) {
	for fixture, want := range map[string]bool{statusRunning: true, statusNoCerts: false} {
		s, err := ReadStatus(context.Background(), fixedRunner(map[string]string{"status --json": fixture}, nil))
		if err != nil {
			t.Fatal(err)
		}
		if got := s.HasCertDomain("node.example-tailnet.ts.net"); got != want {
			t.Errorf("HasCertDomain = %v, want %v", got, want)
		}
	}
}

func TestWithOriginDeduplicates(t *testing.T) {
	got := WithOrigin([]string{"http://127.0.0.1:8787", " https://node.example-tailnet.ts.net", ""}, "node.example-tailnet.ts.net")
	want := []string{"http://127.0.0.1:8787", "https://node.example-tailnet.ts.net"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
	if got := WithOrigin([]string{"http://127.0.0.1:8787"}, ""); !reflect.DeepEqual(got, []string{"http://127.0.0.1:8787"}) {
		t.Fatalf("empty host changed origins: %v", got)
	}
}

// The serve fixtures below are derived from Tailscale's ipn/serve.go
// (ServeConfig{TCP, Web map[HostPort]*WebServerConfig{Handlers}, AllowFunnel})
// and the Proxy values ExpandProxyTargetValue stores. They are source-derived,
// not observed: the development host has no serve config (`{}`).
const serveEmpty = `{}`

// Source-derived, not observed: `tailscale serve --bg 8787`.
const serveBridge = `{
  "TCP": {"443": {"HTTPS": true}},
  "Web": {"node.example-tailnet.ts.net:443": {"Handlers": {"/": {"Proxy": "http://127.0.0.1:8787"}}}}
}`

// Source-derived, not observed: the same proxy with Funnel enabled.
const serveFunnel = `{
  "TCP": {"443": {"HTTPS": true}},
  "Web": {"node.example-tailnet.ts.net:443": {"Handlers": {"/": {"Proxy": "http://127.0.0.1:8787"}}}},
  "AllowFunnel": {"node.example-tailnet.ts.net:443": true}
}`

// Source-derived, not observed: a proxy to an unrelated local port and path.
const serveOther = `{
  "TCP": {"443": {"HTTPS": true}},
  "Web": {"node.example-tailnet.ts.net:443": {"Handlers": {"/": {"Proxy": "http://127.0.0.1:3000"}, "/docs": {"Path": "/srv/docs"}}}},
  "AllowFunnel": {"node.example-tailnet.ts.net:443": false}
}`

// Source-derived, not observed: `tailscale funnel 8787` without --bg. The CLI
// keeps its config under Foreground[<session ID>] while it runs and leaves the
// background Web and AllowFunnel untouched.
const serveForegroundFunnel = `{
  "Foreground": {
    "0123456789abcdef": {
      "TCP": {"443": {"HTTPS": true}},
      "Web": {"node.example-tailnet.ts.net:443": {"Handlers": {"/": {"Proxy": "http://127.0.0.1:8787"}}}},
      "AllowFunnel": {"node.example-tailnet.ts.net:443": true}
    }
  }
}`

func TestServeConfigProxyAndFunnel(t *testing.T) {
	const host = "node.example-tailnet.ts.net"
	cases := []struct {
		name          string
		fixture       string
		proxy, funnel bool
	}{
		{"empty", serveEmpty, false, false},
		{"bridge", serveBridge, true, false},
		{"funnel", serveFunnel, true, true},
		{"other", serveOther, false, false},
		// Foreground proxies are not durable; Foreground Funnel still exposes Bridge.
		{"foreground funnel", serveForegroundFunnel, false, true},
	}
	for _, tc := range cases {
		c, err := ReadServeConfig(context.Background(), fixedRunner(map[string]string{"serve status --json": tc.fixture}, nil))
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got := c.ProxiesTo(host, "127.0.0.1:8787"); got != tc.proxy {
			t.Errorf("%s: ProxiesTo = %v", tc.name, got)
		}
		if got := c.FunnelEnabled(host); got != tc.funnel {
			t.Errorf("%s: FunnelEnabled = %v", tc.name, got)
		}
		if c.ProxiesTo("other.example-tailnet.ts.net", "127.0.0.1:8787") {
			t.Errorf("%s: proxy matched another host", tc.name)
		}
		if c.FunnelEnabled("other.example-tailnet.ts.net") {
			t.Errorf("%s: Funnel matched another host", tc.name)
		}
		if ports := c.TCPForwardsTo("127.0.0.1:8787"); len(ports) != 0 {
			t.Errorf("%s: HTTPS handler reported as TCP forward: %v", tc.name, ports)
		}
	}
}

// Source-derived from ipn.TCPPortHandler and the CLI's SetTCPForwarding, not
// observed: `tailscale serve --bg --tcp 10000 tcp://127.0.0.1:8787` and
// `--tls-terminated-tcp 8443 8787` next to the HTTPS proxy, plus forwards to
// an unrelated port and a unix socket. The CLI stores the target's host:port.
const serveTCPBridge = `{
  "TCP": {
    "443": {"HTTPS": true},
    "10000": {"TCPForward": "127.0.0.1:8787"},
    "8443": {"TCPForward": "127.0.0.1:8787", "TerminateTLS": "node.example-tailnet.ts.net"},
    "5432": {"TCPForward": "127.0.0.1:5432"},
    "9000": {"TCPForward": "unix:/tmp/bridge.sock"}
  },
  "Web": {"node.example-tailnet.ts.net:443": {"Handlers": {"/": {"Proxy": "http://127.0.0.1:8787"}}}}
}`

// Source-derived, not observed: `tailscale serve --tcp 8787 tcp://localhost:8787`
// without --bg.
const serveForegroundTCP = `{
  "Foreground": {
    "0123456789abcdef": {"TCP": {"8787": {"TCPForward": "localhost:8787"}}}
  }
}`

// Source-derived from ipn.ServeConfig.Services (map of "svc:<name>" to a
// ServiceConfig with its own TCP handlers), not observed:
// `tailscale serve --service=svc:web --tcp 10000 tcp://127.0.0.1:8787`.
const serveServiceTCP = `{
  "Services": {
    "svc:web": {"TCP": {"10000": {"TCPForward": "127.0.0.1:8787"}, "443": {"HTTPS": true}}},
    "svc:db": {"TCP": {"5432": {"TCPForward": "127.0.0.1:5432"}}}
  }
}`

func TestServeConfigTCPForward(t *testing.T) {
	cases := []struct {
		name, fixture, listen string
		want                  []string
	}{
		{"background", serveTCPBridge, "127.0.0.1:8787", []string{"10000", "8443"}},
		{"foreground", serveForegroundTCP, "127.0.0.1:8787", []string{"8787"}},
		{"service", serveServiceTCP, "127.0.0.1:8787", []string{"svc:web:10000"}},
		{"other listen port", serveTCPBridge, "127.0.0.1:8788", []string{}},
		{"other listen family", serveTCPBridge, "[::1]:8787", []string{}},
	}
	for _, tc := range cases {
		c, err := ReadServeConfig(context.Background(), fixedRunner(map[string]string{"serve status --json": tc.fixture}, nil))
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got := c.TCPForwardsTo(tc.listen); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: TCPForwardsTo = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestProxyTargetNormalization(t *testing.T) {
	for proxy, want := range map[string]bool{
		"http://127.0.0.1:8787":           true,
		"http://127.0.0.1:8787/":          true,
		"http://localhost:8787":           true,
		"127.0.0.1:8787":                  true,
		"localhost:8787":                  true,
		"8787":                            true,
		"http://127.0.0.1:8788":           false,
		"https://127.0.0.1:8787":          false,
		"https+insecure://127.0.0.1:8787": false,
		"http://127.0.0.1:8787/api":       false,
		"http://10.0.0.1:8787":            false,
		"http://[::1]:8787":               false,
		"unix:/tmp/bridge.sock":           false,
		"":                                false,
	} {
		if got := proxyTargets(proxy, "127.0.0.1:8787"); got != want {
			t.Errorf("proxyTargets(%q) = %v, want %v", proxy, got, want)
		}
	}
	if !proxyTargets("http://127.0.0.1:8787", "localhost:8787") || !proxyTargets("http://[::1]:8787", "[::1]:8787") {
		t.Error("loopback listen aliases rejected")
	}
}

func TestFindCLI(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "tailscale")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	plain := filepath.Join(dir, "plain")
	if err := os.WriteFile(plain, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	notFound := func(string) (string, error) { return "", errors.New("not found") }
	if got, err := findCLI("", notFound, []string{filepath.Join(dir, "missing"), plain, bin}); err != nil || got != bin {
		t.Fatalf("well-known fallback: %q %v", got, err)
	}
	if _, err := findCLI("", notFound, []string{plain, dir}); err == nil {
		t.Fatal("non-executable candidates accepted")
	}
	onPath := func(name string) (string, error) { return "/on/path/" + name, nil }
	if got, _ := findCLI("", onPath, []string{bin}); got != "/on/path/tailscale" {
		t.Fatalf("PATH must win over well-known paths: %q", got)
	}
	if got, err := findCLI(bin, notFound, nil); err != nil || got != bin {
		t.Fatalf("explicit path: %q %v", got, err)
	}
	if _, err := findCLI(plain, onPath, []string{bin}); err == nil {
		t.Fatal("explicit non-executable path accepted")
	}
}

func TestDropUnverifiedOrigins(t *testing.T) {
	const host = "node.example-tailnet.ts.net"
	origins := []string{"http://127.0.0.1:8787", "https://" + host, "https://other.example-tailnet.ts.net", "https://*.ts.net", "null", "https://localhost:8443"}
	cases := []struct {
		name          string
		id            Identity
		kept, dropped []string
	}{
		{"owner resolved", Identity{Host: host, HostSource: SourceAuto, Login: "owner@example.com", LoginSource: SourceAuto}, origins, nil},
		{"login unresolved", Identity{Host: host, HostSource: SourceExplicit, LoginSource: SourceUnavailable},
			[]string{"http://127.0.0.1:8787", "https://other.example-tailnet.ts.net", "https://*.ts.net", "null", "https://localhost:8443"}, []string{"https://" + host}},
		{"auto host unresolved", Identity{HostSource: SourceUnavailable, LoginSource: SourceUnavailable},
			[]string{"http://127.0.0.1:8787", "https://*.ts.net", "null", "https://localhost:8443"}, []string{"https://" + host, "https://other.example-tailnet.ts.net"}},
		{"off", Identity{HostSource: SourceOff, LoginSource: SourceOff}, origins, nil},
	}
	for _, tc := range cases {
		kept, dropped := tc.id.DropUnverifiedOrigins(origins)
		if !reflect.DeepEqual(kept, tc.kept) || !reflect.DeepEqual(dropped, tc.dropped) {
			t.Errorf("%s: kept %v dropped %v", tc.name, kept, dropped)
		}
	}
}

// The macOS /usr/local/bin/tailscale wrapper runs the app binary without
// exec (the trailing command keeps sh from exec-ing sleep as a tail call). A
// context kill then only hits the shell while the grandchild keeps
// stdout open; the runner must still return after WaitDelay.
func TestExecRunnerReturnsWhenGrandchildHoldsPipe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a POSIX shell")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh unavailable")
	}
	script := filepath.Join(t.TempDir(), "tailscale")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 5\nexit $?\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	const timeout, waitDelay = 200 * time.Millisecond, 200 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	start := time.Now()
	_, err := execRunner(script, waitDelay, maxOutputBytes)(ctx, "status", "--json")
	if elapsed := time.Since(start); elapsed > timeout+waitDelay+time.Second {
		t.Fatalf("runner blocked for %v", elapsed)
	}
	if err == nil {
		t.Fatal("canceled run reported success")
	}
}

func TestExecRunnerRejectsOversizedOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a POSIX shell")
	}
	script := filepath.Join(t.TempDir(), "tailscale")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf 0123456789\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := execRunner(script, execWaitDelay, 4)(context.Background()); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized output: %v", err)
	}
	if out, err := execRunner(script, execWaitDelay, 64)(context.Background()); err != nil || string(out) != "0123456789" {
		t.Fatalf("bounded output: %q %v", out, err)
	}
}
