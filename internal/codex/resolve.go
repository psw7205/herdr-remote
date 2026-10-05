package codex

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

var nativeID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// maxSessionMetaBytes bounds the first line, which carries the base
// instructions (tens of KiB in observed rollouts).
const maxSessionMetaBytes = 4 << 20

// Resolve accepts only the native ID that Herdr's Codex hook reported. A live
// session writes under sessions/YYYY/MM/DD; archived_sessions holds finished
// threads and is not searched, so a stale copy there is never attached. The
// file must be the only match and must open with that session's session_meta.
func Resolve(root, id string) (string, error) {
	if !nativeID.MatchString(id) {
		return "", errors.New("invalid Codex native session ID")
	}
	paths, err := filepath.Glob(filepath.Join(root, "sessions", "*", "*", "*", "rollout-*-"+id+".jsonl"))
	if err != nil {
		return "", err
	}
	if len(paths) != 1 {
		return "", fmt.Errorf("expected one Codex rollout for native session, found %d", len(paths))
	}
	if err = verifySessionMeta(paths[0], id); err != nil {
		return "", err
	}
	return paths[0], nil
}

func verifySessionMeta(path, id string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	line, err := bufio.NewReader(io.LimitReader(f, maxSessionMetaBytes)).ReadBytes('\n')
	if err != nil {
		return fmt.Errorf("Codex rollout has no complete session_meta: %w", err)
	}
	var first struct {
		Type    string `json:"type"`
		Payload struct {
			ID string `json:"id"`
		} `json:"payload"`
	}
	if err = json.Unmarshal(line, &first); err != nil {
		return fmt.Errorf("decode Codex session_meta: %w", err)
	}
	if first.Type != "session_meta" || first.Payload.ID != id {
		return ErrSessionMismatch
	}
	return nil
}
