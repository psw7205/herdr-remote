package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/psw7205/herdr-remote/internal/herdr"
	"github.com/psw7205/herdr-remote/internal/tailnet"
	"maps"
	"slices"
	"strings"
	"testing"
)

type fixtureReader struct {
	processErr error
	binding    bool
	agents     []herdr.Agent
}

func (f fixtureReader) Snapshot(context.Context) (herdr.Snapshot, error) {
	if f.agents != nil {
		return herdr.Snapshot{Version: "0.9.5", Protocol: 22, Agents: f.agents}, nil
	}
	return herdr.Snapshot{Version: "0.9.1", Protocol: 22, Agents: []herdr.Agent{
		{PaneID: "w1:p1", TerminalID: "term_a", Agent: "claude", Status: "done"},
		{PaneID: "w1:p2", TerminalID: "term_b", Agent: "claude", Session: &herdr.NativeSession{Agent: "claude", Kind: "id", Value: "native-b"}},
	}}, nil
}
func (f fixtureReader) ProcessInfo(_ context.Context, id string) (herdr.ProcessInfo, error) {
	if f.processErr != nil {
		return herdr.ProcessInfo{}, f.processErr
	}
	return herdr.ProcessInfo{PaneID: id, ForegroundProcesses: []herdr.Process{{PID: 21, Name: "claude"}}}, nil
}
func TestDoctorReportsBlockersWithoutInventingCapabilities(t *testing.T) {
	var out bytes.Buffer
	if err := diagnose(context.Background(), fixtureReader{}, true, tailnetReport{}, &out); err != nil {
		t.Fatal(err)
	}
	var got report
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.WriteEnabled || got.TerminalMirrorEnabled || got.Agents[0].VerifiedBinding || got.Agents[1].VerifiedBinding {
		t.Fatalf("write capability or an unverified session misreported: %+v", got)
	}
	if len(got.Agents) != 2 || got.Agents[0].NativeAssociation || !got.Agents[1].NativeAssociation {
		t.Fatalf("incorrect association report: %+v", got)
	}
	if len(got.Blockers) != 1 || !strings.Contains(got.Blockers[0], "No verified native runtime binding") {
		t.Fatalf("blockers %v", got.Blockers)
	}
	if bytes.Contains(out.Bytes(), []byte("native-b")) {
		t.Fatal("doctor unnecessarily prints native session identity")
	}
}
func diagnoseReport(t *testing.T, f fixtureReader, supported bool) (report, []byte) {
	t.Helper()
	var out bytes.Buffer
	if err := diagnose(context.Background(), f, supported, tailnetReport{}, &out); err != nil {
		t.Fatal(err)
	}
	var got report
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	return got, out.Bytes()
}

func TestDoctorWriteEnabledRule(t *testing.T) {
	codexOnly := []herdr.Agent{{PaneID: "w1:p3", TerminalID: "term_c", Agent: "codex"}}
	for _, tc := range []struct {
		name      string
		reader    fixtureReader
		supported bool
		want      bool
	}{
		{"Claude pane answered process_info", fixtureReader{}, true, true},
		{"no Claude pane", fixtureReader{agents: codexOnly}, true, true},
		{"no agents", fixtureReader{agents: []herdr.Agent{}}, true, true},
		{"process_info failed for every Claude pane", fixtureReader{processErr: errors.New("gone")}, true, false},
		{"platform cannot read start times", fixtureReader{binding: true}, false, false},
		{"platform unsupported without Claude pane", fixtureReader{agents: codexOnly}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := diagnoseReport(t, tc.reader, tc.supported)
			if got.WriteEnabled != tc.want {
				t.Fatalf("write_enabled = %v, want %v: %+v", got.WriteEnabled, tc.want, got)
			}
			platform := slices.ContainsFunc(got.Blockers, func(b string) bool { return strings.Contains(b, "process start times") })
			if platform == tc.supported {
				t.Fatalf("platform blocker %v with support %v: %v", platform, tc.supported, got.Blockers)
			}
		})
	}
}

func TestDoctorReportFieldsAreExact(t *testing.T) {
	_, raw := diagnoseReport(t, fixtureReader{binding: true}, true)
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatal(err)
	}
	keys := slices.Sorted(maps.Keys(top))
	if want := []string{"agents", "blockers", "herdr_protocol", "herdr_version", "tailnet", "terminal_mirror_enabled", "write_enabled"}; !slices.Equal(keys, want) {
		t.Fatalf("report keys %v, want %v", keys, want)
	}
	var agents []map[string]json.RawMessage
	if err := json.Unmarshal(top["agents"], &agents); err != nil || len(agents) != 2 {
		t.Fatalf("agents %s %v", top["agents"], err)
	}
	if _, ok := agents[1]["verified_binding"]; !ok {
		t.Fatalf("agent report lacks verified_binding: %v", agents[1])
	}
}

func TestDoctorReportsProcessFailurePerPane(t *testing.T) {
	var out bytes.Buffer
	if err := diagnose(context.Background(), fixtureReader{processErr: errors.New("gone")}, true, tailnetReport{}, &out); err != nil {
		t.Fatal(err)
	}
	var got report
	json.Unmarshal(out.Bytes(), &got)
	if got.Agents[0].ProcessError == "" || got.Agents[0].ProcessCount != 0 {
		t.Fatal("process failure hidden")
	}
}

func (f fixtureReader) TerminalSnapshot(_ context.Context, id string) (herdr.TerminalSnapshot, error) {
	return herdr.TerminalSnapshot{PaneID: id, Source: "visible", Format: "ansi", Text: "private terminal output"}, nil
}
func (f fixtureReader) Binding(_ context.Context, id string) (herdr.Binding, error) {
	if !f.binding || id != "w1:p2" {
		return herdr.Binding{}, errors.New("binding unavailable")
	}
	return herdr.Binding{Token: "verified", TerminalID: "term_b", NativeSessionID: "native-b", ProcessID: 21, Agent: "claude"}, nil
}
func TestDoctorDetectsBoundInputWithoutPrintingToken(t *testing.T) {
	var out bytes.Buffer
	if err := diagnose(context.Background(), fixtureReader{binding: true}, true, tailnetReport{}, &out); err != nil {
		t.Fatal(err)
	}
	var got report
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.WriteEnabled || !got.TerminalMirrorEnabled || !got.Agents[1].VerifiedBinding {
		t.Fatalf("binding omitted: %+v", got)
	}
	if len(got.Blockers) != 0 {
		t.Fatalf("unexpected blockers: %v", got.Blockers)
	}
	if bytes.Contains(out.Bytes(), []byte(`"verified"`)) {
		t.Fatal("binding token disclosed")
	}
}
func TestDoctorReportsVisibleReadWithoutOutputDisclosure(t *testing.T) {
	var out bytes.Buffer
	if err := diagnose(context.Background(), fixtureReader{}, true, tailnetReport{}, &out); err != nil {
		t.Fatal(err)
	}
	var got report
	json.Unmarshal(out.Bytes(), &got)
	if !got.Agents[0].VisibleSnapshotAvailable {
		t.Fatal("visible snapshot not inspected")
	}
	if bytes.Contains(out.Bytes(), []byte("private terminal output")) {
		t.Fatal("terminal text disclosed")
	}
}

// Anonymized `tailscale status --json` subset; all values are synthetic.
const doctorStatus = `{"BackendState":"Running","CertDomains":%s,"Self":{"DNSName":"node.example-tailnet.ts.net.","UserID":7%s},"User":{"7":{"ID":7,"LoginName":"%s"}}}`

// Source-derived from Tailscale ipn/serve.go (ServeConfig.Web/AllowFunnel), not
// observed: the development host has no serve config.
const doctorServeBridge = `{"TCP":{"443":{"HTTPS":true}},"Web":{"node.example-tailnet.ts.net:443":{"Handlers":{"/":{"Proxy":"http://127.0.0.1:8787"}}}}%s}`

func tailnetRunner(status, serve string) tailnet.Runner {
	return func(_ context.Context, args ...string) ([]byte, error) {
		switch strings.Join(args, " ") {
		case "status --json":
			return []byte(status), nil
		case "serve status --json":
			return []byte(serve), nil
		}
		return nil, errors.New("mutating or unexpected tailscale call: " + strings.Join(args, " "))
	}
}

func TestInspectTailnetReportsMissingSetupAsIssues(t *testing.T) {
	status := fmt.Sprintf(doctorStatus, "null", "", "owner@example.com")
	tn := inspectTailnet(context.Background(), tailnetRunner(status, "{}"), nil, "127.0.0.1:8787")
	if !tn.CLIAvailable || tn.BackendState != "Running" || tn.Host != "node.example-tailnet.ts.net" || tn.Login != "owner@example.com" {
		t.Fatalf("identity: %+v", tn)
	}
	if tn.HTTPSCertificates || tn.ServeProxy || tn.Funnel || len(tn.Issues) != 2 {
		t.Fatalf("setup: %+v", tn)
	}
	if !strings.Contains(tn.Issues[0], "HTTPS Certificates") || !strings.Contains(tn.Issues[1], "tailscale serve --bg 8787") {
		t.Fatalf("issues not actionable: %v", tn.Issues)
	}
	var out bytes.Buffer
	if err := diagnose(context.Background(), fixtureReader{binding: true}, true, tn, &out); err != nil {
		t.Fatal(err)
	}
	var got report
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Blockers) != 0 || len(got.Tailnet.Issues) != 2 {
		t.Fatalf("missing tailnet setup must not block localhost use: %+v", got)
	}
}

func TestInspectTailnetReadyAndTagged(t *testing.T) {
	certs := `["node.example-tailnet.ts.net"]`
	tn := inspectTailnet(context.Background(), tailnetRunner(fmt.Sprintf(doctorStatus, certs, "", "owner@example.com"), fmt.Sprintf(doctorServeBridge, "")), nil, "127.0.0.1:8787")
	if !tn.HTTPSCertificates || !tn.ServeProxy || tn.Funnel || len(tn.Issues) != 0 {
		t.Fatalf("ready tailnet reported issues: %+v", tn)
	}
	tn = inspectTailnet(context.Background(), tailnetRunner(fmt.Sprintf(doctorStatus, certs, `,"Tags":["tag:server"]`, "tagged-devices"), fmt.Sprintf(doctorServeBridge, "")), nil, "127.0.0.1:8787")
	if tn.Host == "" || tn.Login != "" || len(tn.Issues) != 1 || !strings.Contains(tn.Issues[0], "-tailnet-login") {
		t.Fatalf("tagged node: %+v", tn)
	}
}

func TestDoctorBlocksFunnel(t *testing.T) {
	certs := `["node.example-tailnet.ts.net"]`
	serve := fmt.Sprintf(doctorServeBridge, `,"AllowFunnel":{"node.example-tailnet.ts.net:443":true}`)
	tn := inspectTailnet(context.Background(), tailnetRunner(fmt.Sprintf(doctorStatus, certs, "", "owner@example.com"), serve), nil, "127.0.0.1:8787")
	if !tn.Funnel {
		t.Fatalf("funnel missed: %+v", tn)
	}
	var out bytes.Buffer
	if err := diagnose(context.Background(), fixtureReader{binding: true}, true, tn, &out); err != nil {
		t.Fatal(err)
	}
	var got report
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Blockers) != 1 || !strings.Contains(got.Blockers[0], "Funnel") {
		t.Fatalf("Funnel must be a blocker: %v", got.Blockers)
	}
}

// Source-derived from ipn.TCPPortHandler, not observed: the HTTPS proxy plus
// one extra raw TCP forward (`tailscale serve --bg --tcp <port> <target>`).
const doctorServeTCP = `{"TCP":{"443":{"HTTPS":true},%s},"Web":{"node.example-tailnet.ts.net:443":{"Handlers":{"/":{"Proxy":"http://127.0.0.1:8787"}}}}}`

// Raw TCP forwards to the Bridge listen address, in background and foreground
// config, must block like Funnel.
func TestDoctorBlocksTCPForwardToBridge(t *testing.T) {
	certs := `["node.example-tailnet.ts.net"]`
	status := fmt.Sprintf(doctorStatus, certs, "", "owner@example.com")
	cases := []struct {
		name, serve string
		port        string
	}{
		{"background", fmt.Sprintf(doctorServeTCP, `"10000":{"TCPForward":"127.0.0.1:8787"}`), "10000"},
		{"foreground", fmt.Sprintf(doctorServeBridge, `,"Foreground":{"0123456789abcdef":{"TCP":{"8787":{"TCPForward":"localhost:8787","TerminateTLS":"node.example-tailnet.ts.net"}}}}`), "8787"},
	}
	for _, tc := range cases {
		tn := inspectTailnet(context.Background(), tailnetRunner(status, tc.serve), nil, "127.0.0.1:8787")
		if !tn.TCPForward || len(tn.TCPForwardPorts) != 1 || tn.TCPForwardPorts[0] != tc.port || !tn.ServeProxy || len(tn.Issues) != 0 {
			t.Fatalf("%s: TCP forward missed: %+v", tc.name, tn)
		}
		var out bytes.Buffer
		if err := diagnose(context.Background(), fixtureReader{binding: true}, true, tn, &out); err != nil {
			t.Fatal(err)
		}
		var got report
		if err := json.Unmarshal(out.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if len(got.Blockers) != 1 || !strings.Contains(got.Blockers[0], tc.port) || !strings.Contains(got.Blockers[0], "--tcp=<port> off") || !strings.Contains(got.Blockers[0], "tailscale serve --bg") {
			t.Fatalf("%s: TCP forward must be a blocker: %v", tc.name, got.Blockers)
		}
		if !bytes.Contains(out.Bytes(), []byte(`"tcp_forward": true`)) {
			t.Fatalf("%s: tcp_forward not reported: %s", tc.name, out.Bytes())
		}
	}
}

func TestDoctorIgnoresTCPForwardToOtherPort(t *testing.T) {
	certs := `["node.example-tailnet.ts.net"]`
	serve := fmt.Sprintf(doctorServeTCP, `"5432":{"TCPForward":"127.0.0.1:5432"}`)
	tn := inspectTailnet(context.Background(), tailnetRunner(fmt.Sprintf(doctorStatus, certs, "", "owner@example.com"), serve), nil, "127.0.0.1:8787")
	if tn.TCPForward || len(tn.TCPForwardPorts) != 0 || !tn.ServeProxy || len(tn.Issues) != 0 {
		t.Fatalf("unrelated TCP forward reported: %+v", tn)
	}
	var out bytes.Buffer
	if err := diagnose(context.Background(), fixtureReader{binding: true}, true, tn, &out); err != nil {
		t.Fatal(err)
	}
	var got report
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Blockers) != 0 {
		t.Fatalf("unexpected blockers: %v", got.Blockers)
	}
}

func TestInspectTailnetWithoutCLI(t *testing.T) {
	tn := inspectTailnet(context.Background(), nil, errors.New("tailscale CLI not found"), "127.0.0.1:8787")
	if tn.CLIAvailable || tn.Host != "" || len(tn.Issues) != 1 {
		t.Fatalf("missing CLI: %+v", tn)
	}
	stopped := inspectTailnet(context.Background(), tailnetRunner(`{"BackendState":"Stopped"}`, "{}"), nil, "127.0.0.1:8787")
	if !stopped.CLIAvailable || stopped.BackendState != "Stopped" || stopped.Host != "" || len(stopped.Issues) != 1 || !strings.Contains(stopped.Issues[0], "tailscale up") {
		t.Fatalf("stopped backend: %+v", stopped)
	}
}

// Without a MagicDNS name the HTTPS proxy cannot be checked, but Funnel and raw
// TCP forwards (here on a Tailscale Service) still expose Bridge.
func TestDoctorBlocksExposureWithoutMagicDNS(t *testing.T) {
	status := `{"BackendState":"Running","Self":{"DNSName":"","UserID":7},"User":{"7":{"ID":7,"LoginName":"owner@example.com"}}}`
	serve := `{"AllowFunnel":{"node.example-tailnet.ts.net:443":true},"Services":{"svc:web":{"TCP":{"10000":{"TCPForward":"127.0.0.1:8787"}}}}}`
	tn := inspectTailnet(context.Background(), tailnetRunner(status, serve), nil, "127.0.0.1:8787")
	if tn.Host != "" || !tn.Funnel || !tn.TCPForward || len(tn.TCPForwardPorts) != 1 || tn.TCPForwardPorts[0] != "svc:web:10000" {
		t.Fatalf("exposure missed without host: %+v", tn)
	}
	if len(tn.Issues) != 1 || !strings.Contains(tn.Issues[0], "MagicDNS") {
		t.Fatalf("issues: %v", tn.Issues)
	}
	var out bytes.Buffer
	if err := diagnose(context.Background(), fixtureReader{binding: true}, true, tn, &out); err != nil {
		t.Fatal(err)
	}
	var got report
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Blockers) != 2 || !strings.Contains(got.Blockers[0], "Funnel is enabled for this node") || !strings.Contains(got.Blockers[1], "--service=svc:<name>") {
		t.Fatalf("blockers: %v", got.Blockers)
	}
	tn = inspectTailnet(context.Background(), tailnetRunner(status, "{}"), nil, "127.0.0.1:8787")
	if tn.Funnel || tn.TCPForward || len(tn.Issues) != 1 {
		t.Fatalf("clean serve without host: %+v", tn)
	}
}
