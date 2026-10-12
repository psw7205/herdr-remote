package command

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func request() Request {
	return Request{CommandID: "00000000-0000-4000-8000-000000000001", SessionID: "s", RuntimeBinding: "b", Type: "prompt", Payload: Payload{Text: "hello"}}
}
func TestConcurrentRetriesDispatchOnce(t *testing.T) {
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	var calls atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Execute(context.Background(), request(), func(context.Context, Request) Result { calls.Add(1); return Result{Status: "accepted"} })
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("dispatched %d", calls.Load())
	}
}
func TestRestartAfterReserveNeverRedispatches(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir)
	r := request()
	if _, owned, e := s.reserve(r); e != nil || !owned {
		t.Fatal(owned, e)
	}
	restarted, _ := Open(dir)
	got := restarted.Execute(context.Background(), r, func(context.Context, Request) Result { t.Fatal("duplicate after crash"); return Result{} })
	if got.Status != "delivery_unknown" {
		t.Fatalf("got %+v", got)
	}
}
func TestReusedIDWithDifferentPayloadRejected(t *testing.T) {
	s, _ := Open(t.TempDir())
	r := request()
	s.Execute(context.Background(), r, func(context.Context, Request) Result { return Result{Status: "accepted"} })
	r.Payload.Text = "different"
	got := s.Execute(context.Background(), r, func(context.Context, Request) Result { t.Fatal("conflict dispatched"); return Result{} })
	if got.Status != "rejected" || got.Code != "COMMAND_ID_CONFLICT" {
		t.Fatalf("got %+v", got)
	}
}
func TestUnknownResultDoesNotRetry(t *testing.T) {
	s, _ := Open(t.TempDir())
	r := request()
	s.Execute(context.Background(), r, func(context.Context, Request) Result { return Result{Status: "delivery_unknown"} })
	s.Execute(context.Background(), r, func(context.Context, Request) Result { t.Fatal("unknown retried"); return Result{} })
}
func TestCommandIDCannotEscapeDirectory(t *testing.T) {
	s, _ := Open(t.TempDir())
	r := request()
	r.CommandID = "../escape"
	got := s.Execute(context.Background(), r, func(context.Context, Request) Result { t.Fatal("invalid ID dispatched"); return Result{} })
	if got.Status != "rejected" {
		t.Fatal(got)
	}
}
func TestPromptDigestInputUnchangedBySessionStartFields(t *testing.T) {
	b, err := json.Marshal(request())
	if err != nil {
		t.Fatal(err)
	}
	// Receipts on disk were written with this exact encoding.
	want := `{"command_id":"00000000-0000-4000-8000-000000000001","session_id":"s","runtime_binding":"b","command_type":"prompt","payload":{"text":"hello"}}`
	if string(b) != want {
		t.Fatalf("prompt encoding changed:\n%s", b)
	}
}
func TestSessionStartRetryReturnsCreatedTopology(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir)
	r := Request{CommandID: "00000000-0000-4000-8000-000000000002", Type: "session_start", Payload: Payload{CandidateID: "c1", Kind: "claude", Placement: "new_workspace"}}
	created := &Created{WorkspaceID: "w2", TabID: "w2:t1", PaneID: "w2:p1", Agent: "mobile-000000000000"}
	first := s.Execute(context.Background(), r, func(context.Context, Request) Result {
		return Result{Status: "accepted", Code: "AGENT_NOT_READY", Created: created}
	})
	restarted, _ := Open(dir)
	again := restarted.Execute(context.Background(), r, func(context.Context, Request) Result { t.Fatal("start dispatched twice"); return Result{} })
	if first.Created == nil || again.Created == nil || *again.Created != *created || again.Code != "AGENT_NOT_READY" {
		t.Fatalf("first %+v again %+v", first, again)
	}
	r.Payload.Placement = "new_tab"
	if got := restarted.Execute(context.Background(), r, func(context.Context, Request) Result { t.Fatal("conflict dispatched"); return Result{} }); got.Code != "COMMAND_ID_CONFLICT" {
		t.Fatalf("placement change: %+v", got)
	}
}
func exists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Lstat(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	return err == nil
}
func TestOpenRemovesOnlyTempFiles(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	r := request()
	if got := s.Execute(context.Background(), r, func(context.Context, Request) Result { return Result{Status: "accepted"} }); got.Status != "accepted" {
		t.Fatalf("got %+v", got)
	}
	temps := []string{"receipt-", "receipt-abc", "receipt-123456789"}
	for _, name := range temps {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(`{"digest":"`), 0600); err != nil {
			t.Fatal(err)
		}
		age(t, filepath.Join(dir, name))
	}
	if err := os.WriteFile(filepath.Join(dir, "receipt-fresh"), []byte(`{"digest":"`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "receipt-dir"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("notes.txt", filepath.Join(dir, "receipt-link")); err != nil {
		t.Fatal(err)
	}
	restarted, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range temps {
		if exists(t, filepath.Join(dir, name)) {
			t.Errorf("%s survived Open", name)
		}
	}
	for _, name := range []string{r.CommandID + ".json", "notes.txt", "receipt-dir", "receipt-link", "receipt-fresh"} {
		if !exists(t, filepath.Join(dir, name)) {
			t.Errorf("Open removed %s", name)
		}
	}
	if got := restarted.Execute(context.Background(), r, func(context.Context, Request) Result { t.Fatal("receipt lost"); return Result{} }); got.Status != "accepted" {
		t.Fatalf("got %+v", got)
	}
}
func TestReopenAfterCrashInFinishKeepsReservation(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	r := request()
	if _, owned, err := s.reserve(r); err != nil || !owned {
		t.Fatal(owned, err)
	}
	temp, err := os.CreateTemp(dir, tempPrefix)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := temp.WriteString(`{"digest":"`); err != nil {
		t.Fatal(err)
	}
	temp.Close()
	age(t, temp.Name())
	restarted, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if exists(t, temp.Name()) {
		t.Fatal("temp file survived Open")
	}
	if got := restarted.Execute(context.Background(), r, func(context.Context, Request) Result { t.Fatal("redispatched after crash"); return Result{} }); got.Status != "delivery_unknown" {
		t.Fatalf("got %+v", got)
	}
	fresh := request()
	fresh.CommandID = "00000000-0000-4000-8000-000000000003"
	if got := restarted.Execute(context.Background(), fresh, func(context.Context, Request) Result { return Result{Status: "accepted"} }); got.Status != "accepted" {
		t.Fatalf("got %+v", got)
	}
	if !exists(t, filepath.Join(dir, fresh.CommandID+".json")) {
		t.Fatal("fresh receipt missing")
	}
	if left, err := filepath.Glob(filepath.Join(dir, tempPrefix+"*")); err != nil || len(left) != 0 {
		t.Fatal(left, err)
	}
}
func TestOpenContinuesWhenTempFileCannotBeRemoved(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "receipt-abc")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	age(t, path)
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0700) })
	if _, err := Open(dir); err != nil {
		t.Fatalf("a leftover temp kept the store from opening: %v", err)
	}
	if os.Geteuid() != 0 && !exists(t, path) {
		t.Fatal("temp vanished from a read-only directory")
	}
}

// age backdates a temp past tempMaxAge so Open treats it as a crash leftover.
func age(t *testing.T, path string) {
	t.Helper()
	old := time.Now().Add(-2 * tempMaxAge)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
}
