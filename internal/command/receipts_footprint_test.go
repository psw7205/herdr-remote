package command

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

var footprintKinds = []string{"prompt", "interrupt", "session_start"}

func footprintRequest(i int) Request {
	r := Request{CommandID: fmt.Sprintf("%08x-0000-4000-8000-%012x", i, i), SessionID: "claude:00000000-0000-4000-8000-00000000000a", RuntimeBinding: "synthetic-binding", Type: footprintKinds[i%len(footprintKinds)]}
	switch r.Type {
	case "prompt":
		r.Payload.Text = "synthetic prompt " + strconv.Itoa(i)
	case "session_start":
		r.SessionID, r.RuntimeBinding = "", ""
		r.Payload = Payload{CandidateID: "candidate-1", Kind: "claude", Placement: "new_tab"}
	}
	return r
}
func footprintResult(r Request) Result {
	if r.Type == "session_start" {
		return Result{Status: "accepted", Code: "AGENT_NOT_READY", Created: &Created{WorkspaceID: "w9", TabID: "w9:t1", PaneID: "w9:p1", Agent: "mobile-" + strings.ReplaceAll(r.CommandID, "-", "")[:12]}}
	}
	return Result{Status: "accepted"}
}

// seedReceipt writes the bytes finish would leave, without its fsyncs, so a
// large directory can be built in seconds. TestSeededReceiptMatchesExecute
// keeps the two encodings identical.
func seedReceipt(dir string, r Request, result Result) error {
	var b bytes.Buffer
	if err := json.NewEncoder(&b).Encode(receipt{Digest: digest(r), Result: result}); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, r.CommandID+".json"), b.Bytes(), 0600)
}
func TestSeededReceiptMatchesExecute(t *testing.T) {
	executed, seeded := t.TempDir(), t.TempDir()
	s, err := Open(executed)
	if err != nil {
		t.Fatal(err)
	}
	for i := range footprintKinds {
		r := footprintRequest(i)
		if got := s.Execute(context.Background(), r, func(context.Context, Request) Result { return footprintResult(r) }); got.Status != "accepted" {
			t.Fatalf("%s: %+v", r.Type, got)
		}
		if err := seedReceipt(seeded, r, footprintResult(r)); err != nil {
			t.Fatal(err)
		}
		want, _ := os.ReadFile(filepath.Join(executed, r.CommandID+".json"))
		got, _ := os.ReadFile(filepath.Join(seeded, r.CommandID+".json"))
		if len(want) == 0 || !bytes.Equal(got, want) {
			t.Fatalf("%s: seeded %q, executed %q", r.Type, got, want)
		}
	}
	entries, err := os.ReadDir(executed)
	if err != nil || len(entries) != len(footprintKinds) {
		t.Fatalf("receipt directory holds %d entries: %v", len(entries), err)
	}
}
func TestReceiptSizeIgnoresPromptText(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	short, long := footprintRequest(0), footprintRequest(3)
	short.Payload.Text = "x"
	long.Payload.Text = strings.Repeat("y", 65536)
	var sizes []int
	for _, r := range []Request{short, long} {
		s.Execute(context.Background(), r, func(context.Context, Request) Result { return Result{Status: "accepted"} })
		b, err := os.ReadFile(filepath.Join(dir, r.CommandID+".json"))
		if err != nil || bytes.Contains(b, []byte(r.Payload.Text)) {
			t.Fatalf("receipt %q: %v", b, err)
		}
		sizes = append(sizes, len(b))
	}
	if sizes[0] != sizes[1] {
		t.Fatalf("receipt size depends on prompt length: %v", sizes)
	}
}

// BenchmarkReceiptFootprint measures disk use and per-call latency against a
// directory of N receipts. Each measurement is one pass; run it with
// -benchtime 1x. The seed cases skip Execute's fsyncs while building the
// directory only; the measured lookups and fresh commands use Execute.
func BenchmarkReceiptFootprint(b *testing.B) {
	cases := []struct {
		populate string
		n        int
	}{{"execute", 1000}, {"execute", 10000}, {"seed", 1000}, {"seed", 10000}, {"seed", 100000}}
	for _, c := range cases {
		b.Run(fmt.Sprintf("%s/N=%d", c.populate, c.n), func(b *testing.B) {
			for range b.N {
				measureReceipts(b, c.populate == "execute", c.n)
			}
		})
	}
}
func measureReceipts(b *testing.B, execute bool, n int) {
	ctx := context.Background()
	dir := filepath.Join(b.TempDir(), "receipts")
	s, err := Open(dir)
	if err != nil {
		b.Fatal(err)
	}
	start := time.Now()
	for i := range n {
		r := footprintRequest(i)
		if !execute {
			if err := seedReceipt(dir, r, footprintResult(r)); err != nil {
				b.Fatal(err)
			}
			continue
		}
		if got := s.Execute(ctx, r, func(context.Context, Request) Result { return footprintResult(r) }); got.Status != "accepted" {
			b.Fatalf("populate %d: %+v", i, got)
		}
	}
	populate := time.Since(start)
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != n {
		b.Fatalf("directory holds %d entries: %v", len(entries), err)
	}
	var logical int64
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			b.Fatal(err)
		}
		logical += info.Size()
	}
	duKiB := diskUsageKiB(b, dir)

	const opens = 100
	start = time.Now()
	for range opens {
		if s, err = Open(dir); err != nil {
			b.Fatal(err)
		}
	}
	open := time.Since(start) / opens

	const lookups = 100
	start = time.Now()
	for i := range lookups {
		r := footprintRequest(i * (n / lookups))
		got := s.Execute(ctx, r, func(context.Context, Request) Result { b.Fatal("existing receipt dispatched"); return Result{} })
		if got.Status != "accepted" {
			b.Fatalf("lookup %s: %+v", r.CommandID, got)
		}
	}
	lookup := time.Since(start) / lookups

	const fresh = 20
	start = time.Now()
	for i := range fresh {
		r := footprintRequest(n + i)
		if got := s.Execute(ctx, r, func(context.Context, Request) Result { return footprintResult(r) }); got.Status != "accepted" {
			b.Fatalf("fresh %s: %+v", r.CommandID, got)
		}
	}
	executeEach := time.Since(start) / fresh

	const syncs = 20
	start = time.Now()
	for range syncs {
		if err := s.syncDir(); err != nil {
			b.Fatal(err)
		}
	}
	syncEach := time.Since(start) / syncs

	b.ReportMetric(float64(logical)/float64(n), "logical-B/file")
	b.ReportMetric(float64(duKiB), "du-KiB")
	b.ReportMetric(float64(duKiB)*1024/float64(n), "du-B/file")
	b.ReportMetric(populate.Seconds(), "populate-s")
	b.ReportMetric(float64(open.Nanoseconds()), "open-ns")
	b.ReportMetric(float64(lookup.Nanoseconds()), "lookup-ns")
	b.ReportMetric(float64(executeEach.Microseconds())/1000, "execute-ms")
	b.ReportMetric(float64(syncEach.Microseconds())/1000, "syncdir-ms")
}
func diskUsageKiB(b *testing.B, dir string) int64 {
	out, err := exec.Command("du", "-sk", dir).Output()
	if err != nil {
		b.Skipf("du unavailable: %v", err)
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		b.Fatalf("du output %q", out)
	}
	kib, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		b.Fatal(err)
	}
	return kib
}
