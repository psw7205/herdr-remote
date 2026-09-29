package command

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
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
