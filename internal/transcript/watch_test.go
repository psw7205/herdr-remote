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
	done := make(chan struct{})
	go func() {
		defer close(done)
		Watch(ctx, path, func(b Batch) error {
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
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watcher did not close")
	}
}

func collect(ctx context.Context, path string) (chan string, chan struct{}) {
	lines := make(chan string, 64)
	done := make(chan struct{})
	go func() {
		defer close(done)
		Watch(ctx, path, func(b Batch) error {
			for _, line := range b.Lines {
				lines <- string(line)
			}
			return nil
		}, func(error) {})
	}()
	return lines, done
}

func expect(t *testing.T, lines chan string, want ...string) {
	t.Helper()
	for _, w := range want {
		select {
		case got := <-lines:
			if got != w {
				t.Fatalf("got %q, want %q", got, w)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("%q missing", w)
		}
	}
}

// A removed and recreated, or replaced, transcript is read from the start.
func TestWatchFollowsRecreatedAndReplacedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "native.jsonl")
	os.WriteFile(path, []byte("first\n"), 0600)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lines, done := collect(ctx, path)
	expect(t, lines, "first")

	os.Remove(path)
	time.Sleep(50 * time.Millisecond)
	os.WriteFile(path, []byte("again\n"), 0600)
	expect(t, lines, "again")

	next := filepath.Join(dir, "next.tmp")
	os.WriteFile(next, []byte("replaced\n"), 0600)
	if err := os.Rename(next, path); err != nil {
		t.Fatal(err)
	}
	expect(t, lines, "replaced")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("appended\n")
	f.Close()
	expect(t, lines, "appended")
	cancel()
	<-done
}
