package transcript

import (
	"context"
	"github.com/fsnotify/fsnotify"
	"log/slog"
	"path/filepath"
	"time"
)

// newWatcher is replaced in tests to exercise the polling fallback.
var newWatcher = fsnotify.NewWatcher

// Watch establishes file observation before the initial read. Polling
// reconciles missed notifications by reading only after the saved offset.
// Cancelling ctx stops reading between batches and closes the fsnotify watcher.
//
// The transcript file itself is watched, not its directory: kqueue opens one
// descriptor per file of a watched directory, and a Claude project directory
// holds every transcript of that project. A file watch does not survive the
// file being removed, renamed or replaced, so it is re-registered on the next
// tick. When fsnotify is unavailable, Watch keeps polling and retries.
func Watch(ctx context.Context, path string, apply func(Batch) error, failed func(error)) {
	var watcher *fsnotify.Watcher
	watching, warned := false, false
	arm := func() {
		if watcher == nil {
			w, err := newWatcher()
			if err != nil {
				if !warned {
					slog.Warn("transcript notifications unavailable; polling", "error", err)
					warned = true
				}
				return
			}
			watcher = w
		}
		if !watching {
			watching = watcher.Add(path) == nil
		}
	}
	disarm := func() {
		if watching {
			_ = watcher.Remove(path)
			watching = false
		}
	}
	defer func() {
		if watcher != nil {
			watcher.Close()
		}
	}()
	arm()
	tail := NewTailer(path)
	first := true
	drain := func() {
		for ctx.Err() == nil {
			batch, e := tail.Read()
			if e != nil {
				failed(e)
				return
			}
			// A replaced file keeps the watch on the old inode.
			if batch.Reset && !first {
				disarm()
				arm()
			}
			first = false
			if e = apply(batch); e != nil {
				failed(e)
				return
			}
			if !batch.More {
				return
			}
		}
	}
	drain()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		// A nil channel blocks, leaving ticks to poll while fsnotify is unavailable.
		var events <-chan fsnotify.Event
		var errs <-chan error
		if watcher != nil {
			events, errs = watcher.Events, watcher.Errors
		}
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			if filepath.Clean(event.Name) != filepath.Clean(path) {
				continue
			}
			if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
				disarm()
				arm()
			}
			drain()
		case err, ok := <-errs:
			if !ok {
				return
			}
			failed(err)
			tail = NewTailer(path)
			first = true
			drain()
		case <-tick.C:
			arm()
			drain()
		}
	}
}
