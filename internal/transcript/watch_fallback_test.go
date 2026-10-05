package transcript

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

type warnCounter struct{ n atomic.Int32 }

func (c *warnCounter) Enabled(context.Context, slog.Level) bool { return true }
func (c *warnCounter) WithAttrs([]slog.Attr) slog.Handler       { return c }
func (c *warnCounter) WithGroup(string) slog.Handler            { return c }
func (c *warnCounter) Handle(_ context.Context, r slog.Record) error {
	if r.Message == "transcript notifications unavailable; polling" {
		c.n.Add(1)
	}
	return nil
}

func captureWarnings(t *testing.T) *warnCounter {
	c := &warnCounter{}
	orig := slog.Default()
	slog.SetDefault(slog.New(c))
	t.Cleanup(func() { slog.SetDefault(orig) })
	return c
}

func stubWatcher(t *testing.T, fails int32) (*atomic.Int32, chan *fsnotify.Watcher) {
	calls := &atomic.Int32{}
	created := make(chan *fsnotify.Watcher, 8)
	orig := newWatcher
	newWatcher = func() (*fsnotify.Watcher, error) {
		if calls.Add(1) <= fails {
			return nil, errors.New("too many open files")
		}
		w, err := fsnotify.NewWatcher()
		if err == nil {
			created <- w
		}
		return w, err
	}
	t.Cleanup(func() { newWatcher = orig })
	return calls, created
}

func watchFallback(ctx context.Context, path string) (chan string, chan error, chan struct{}) {
	lines := make(chan string, 64)
	failures := make(chan error, 8)
	done := make(chan struct{})
	go func() {
		defer close(done)
		Watch(ctx, path, func(b Batch) error {
			for _, line := range b.Lines {
				lines <- string(line)
			}
			return nil
		}, func(e error) { failures <- e })
	}()
	return lines, failures, done
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func appendLine(t *testing.T, path, line string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(line + "\n"); err != nil {
		t.Fatal(err)
	}
}

func TestWatchFallbackPollsWhenNotificationsUnavailable(t *testing.T) {
	warnings := captureWarnings(t)
	calls, _ := stubWatcher(t, 1<<30)
	path := filepath.Join(t.TempDir(), "native.jsonl")
	os.WriteFile(path, []byte("first\n"), 0600)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lines, failures, done := watchFallback(ctx, path)
	expect(t, lines, "first")

	appendLine(t, path, "second")
	expect(t, lines, "second")

	waitFor(t, "repeated watcher attempts", func() bool { return calls.Load() >= 3 })
	if n := warnings.n.Load(); n != 1 {
		t.Fatalf("warned %d times over %d attempts", n, calls.Load())
	}
	cancel()
	<-done
	select {
	case e := <-failures:
		t.Fatalf("failed called: %v", e)
	default:
	}
}

func TestWatchRetriesWatcherCreationAndClosesIt(t *testing.T) {
	warnings := captureWarnings(t)
	calls, created := stubWatcher(t, 1)
	path := filepath.Join(t.TempDir(), "native.jsonl")
	os.WriteFile(path, []byte("first\n"), 0600)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lines, failures, done := watchFallback(ctx, path)
	expect(t, lines, "first")

	var w *fsnotify.Watcher
	select {
	case w = <-created:
	case <-time.After(3 * time.Second):
		t.Fatal("watcher creation not retried")
	}
	waitFor(t, "retried watcher to watch the transcript", func() bool {
		return slices.Contains(w.WatchList(), filepath.Clean(path))
	})
	appendLine(t, path, "second")
	expect(t, lines, "second")

	cancel()
	<-done
	deadline := time.After(time.Second)
	for open := true; open; {
		select {
		case _, open = <-w.Events:
		case <-deadline:
			t.Fatal("retried watcher not closed")
		}
	}
	if n := calls.Load(); n != 2 {
		t.Fatalf("watcher created %d times", n)
	}
	if n := warnings.n.Load(); n != 1 {
		t.Fatalf("warned %d times", n)
	}
	select {
	case e := <-failures:
		t.Fatalf("failed called: %v", e)
	default:
	}
}
