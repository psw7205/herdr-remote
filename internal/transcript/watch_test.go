package transcript

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatcherEmitsCompletedRecordsAndStopsIndependently(t *testing.T) {
	path := filepath.Join(t.TempDir(), "native.jsonl")
	os.WriteFile(path, []byte("first\n"), 0600)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lines := make(chan string, 10)
	done := make(chan error, 1)
	go func() {
		done <- Watch(ctx, path, func(b Batch) error {
			for _, line := range b.Lines {
				lines <- string(line)
			}
			return nil
		}, func(e error) { t.Error(e) })
	}()
	select {
	case got := <-lines:
		if got != "first" {
			t.Fatal(got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("initial missing")
	}
	f, e := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	f.WriteString("partial")
	f.Sync()
	select {
	case got := <-lines:
		t.Fatalf("incomplete record emitted: %s", got)
	case <-time.After(50 * time.Millisecond):
	}
	f.WriteString("\nsecond\n")
	f.Sync()
	for _, want := range []string{"partial", "second"} {
		select {
		case got := <-lines:
			if got != want {
				t.Fatal(got, want)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("append missing")
		}
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("watcher did not close")
	}
}
