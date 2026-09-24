package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"herdr-remote/internal/herdr"
	"strings"
	"testing"
)

type fixtureReader struct {
	processErr  error
	binding     bool
	unsupported bool
}

func (f fixtureReader) Snapshot(context.Context) (herdr.Snapshot, error) {
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
	if err := diagnose(context.Background(), fixtureReader{}, &out); err != nil {
		t.Fatal(err)
	}
	var got report
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.WriteEnabled || got.TerminalMirrorEnabled {
		t.Fatal("unsupported control was enabled")
	}
	if len(got.Agents) != 2 || got.Agents[0].NativeAssociation || !got.Agents[1].NativeAssociation {
		t.Fatalf("incorrect association report: %+v", got)
	}
	if got.ConditionalInput != "unknown" || len(got.Blockers) != 1 || strings.Contains(got.Blockers[0], "herdr-patch.md") {
		t.Fatalf("plain binding errors are not API evidence: %+v", got)
	}
	if bytes.Contains(out.Bytes(), []byte("native-b")) {
		t.Fatal("doctor unnecessarily prints native session identity")
	}
}
func TestDoctorNamesMissingHerdrPatch(t *testing.T) {
	var out bytes.Buffer
	if err := diagnose(context.Background(), fixtureReader{unsupported: true}, &out); err != nil {
		t.Fatal(err)
	}
	var got report
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ConditionalInput != "unsupported" || got.WriteEnabled || got.TerminalMirrorEnabled {
		t.Fatalf("unsupported Herdr enabled controls: %+v", got)
	}
	if len(got.Blockers) != 1 || !strings.Contains(got.Blockers[0], "agent.binding") || !strings.Contains(got.Blockers[0], "docs/herdr-patch.md") {
		t.Fatalf("missing patch blocker: %v", got.Blockers)
	}
}
func TestDoctorReportsProcessFailurePerPane(t *testing.T) {
	var out bytes.Buffer
	if err := diagnose(context.Background(), fixtureReader{processErr: errors.New("gone")}, &out); err != nil {
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
	if f.unsupported {
		return herdr.Binding{}, fmt.Errorf("%w: agent.binding", herdr.ErrUnsupported)
	}
	if !f.binding || id != "w1:p2" {
		return herdr.Binding{}, errors.New("binding unavailable")
	}
	return herdr.Binding{Token: "verified", TerminalID: "term_b", NativeSessionID: "native-b", ProcessID: 21, Agent: "claude"}, nil
}
func TestDoctorDetectsBoundInputWithoutPrintingToken(t *testing.T) {
	var out bytes.Buffer
	if err := diagnose(context.Background(), fixtureReader{binding: true}, &out); err != nil {
		t.Fatal(err)
	}
	var got report
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.WriteEnabled || !got.TerminalMirrorEnabled || !got.Agents[1].VerifiedBinding || got.ConditionalInput != "supported" {
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
	if err := diagnose(context.Background(), fixtureReader{}, &out); err != nil {
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
