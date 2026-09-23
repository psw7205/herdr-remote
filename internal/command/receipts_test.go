package command

import (
	"context"
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
