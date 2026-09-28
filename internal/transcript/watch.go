package transcript

import (
	"context"
	"github.com/fsnotify/fsnotify"
	"path/filepath"
	"time"
)

// Watch establishes directory observation before the initial read. Polling
// reconciles missed notifications by reading only after the saved offset.
// Cancelling ctx stops reading between batches and closes the fsnotify watcher.
func Watch(ctx context.Context, path string, apply func(Batch) error, failed func(error)) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()
	if err = watcher.Add(filepath.Dir(path)); err != nil {
		return err
	}
	tail := NewTailer(path)
	drain := func() {
		for ctx.Err() == nil {
			batch, e := tail.Read()
			if e != nil {
				failed(e)
				return
			}
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
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			if filepath.Clean(event.Name) == filepath.Clean(path) {
				drain()
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			failed(err)
			tail = NewTailer(path)
			drain()
		case <-tick.C:
			drain()
		}
	}
}
