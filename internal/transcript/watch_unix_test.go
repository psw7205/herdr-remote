//go:build unix

package transcript

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func openFDs() int {
	n := 0
	var st syscall.Stat_t
	for fd := range 4096 {
		if syscall.Fstat(fd, &st) == nil {
			n++
		}
	}
	return n
}

// kqueue opens a descriptor per file of a watched directory; a project
// directory holds every transcript of that project.
func TestWatchDoesNotOpenSiblingTranscripts(t *testing.T) {
	dir := t.TempDir()
	for i := range 64 {
		os.WriteFile(filepath.Join(dir, fmt.Sprintf("other-%d.jsonl", i)), []byte("x\n"), 0600)
	}
	path := filepath.Join(dir, "native.jsonl")
	os.WriteFile(path, []byte("first\n"), 0600)
	base := openFDs()
	ctx, cancel := context.WithCancel(context.Background())
	lines, done := collect(ctx, path)
	expect(t, lines, "first")
	if grown := openFDs() - base; grown > 16 {
		t.Fatalf("watch opened %d descriptors", grown)
	}
	cancel()
	<-done
}
