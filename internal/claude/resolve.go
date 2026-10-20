package claude

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
)

var nativeID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// ErrNoTranscript means no transcript exists yet for a well-formed native ID.
// Claude Code creates projects/<dir>/<id>.jsonl with the session's first
// message, so a verified session is in this state until then.
var ErrNoTranscript = errors.New("no Claude transcript for native session")

// Resolve accepts only the native ID supplied by Herdr. Multiple paths with
// that ID are ambiguous even when one is newer or shares the pane's cwd.
func Resolve(root, id string) (string, error) {
	if !nativeID.MatchString(id) {
		return "", errors.New("invalid Claude native session ID")
	}
	paths, err := filepath.Glob(filepath.Join(root, "projects", "*", id+".jsonl"))
	if err != nil {
		return "", err
	}
	switch len(paths) {
	case 1:
		return paths[0], nil
	case 0:
		return "", ErrNoTranscript
	default:
		return "", fmt.Errorf("expected one Claude transcript for native session, found %d", len(paths))
	}
}
