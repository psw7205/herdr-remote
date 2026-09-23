// Package command persists dispatch receipts, never conversation content.
package command

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sync"
)

type Payload struct {
	Text string `json:"text"`
}
type Request struct {
	CommandID      string  `json:"command_id"`
	SessionID      string  `json:"session_id"`
	RuntimeBinding string  `json:"runtime_binding"`
	Type           string  `json:"command_type"`
	Payload        Payload `json:"payload"`
}
type Result struct {
	Status string `json:"status"`
	Code   string `json:"code,omitempty"`
}
type receipt struct {
	Digest string `json:"digest"`
	Result Result `json:"result"`
}
type Store struct {
	mu  sync.Mutex
	dir string
}

var validID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("receipt directory must be a real directory")
	}
	return &Store{dir: dir}, nil
}
func digest(r Request) string {
	b, _ := json.Marshal(r)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func (s *Store) syncDir() error {
	d, e := os.Open(s.dir)
	if e != nil {
		return e
	}
	defer d.Close()
	return d.Sync()
}
func (s *Store) reserve(r Request) (Result, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !validID.MatchString(r.CommandID) {
		return Result{Status: "rejected", Code: "INVALID_COMMAND_ID"}, false, nil
	}
	path := filepath.Join(s.dir, r.CommandID+".json")
	hash := digest(r)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		b, e := os.ReadFile(path)
		var saved receipt
		if e != nil || json.Unmarshal(b, &saved) != nil || saved.Digest == "" {
			return Result{Status: "delivery_unknown", Code: "RECEIPT_UNREADABLE"}, false, nil
		}
		if saved.Digest != hash {
			return Result{Status: "rejected", Code: "COMMAND_ID_CONFLICT"}, false, nil
		}
		return saved.Result, false, nil
	}
	if err != nil {
		return Result{}, false, err
	}
	pending := receipt{Digest: hash, Result: Result{Status: "delivery_unknown"}}
	err = json.NewEncoder(f).Encode(pending)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = s.syncDir()
	}
	// An incomplete reservation is retained as an uncertain delivery barrier.
	return pending.Result, err == nil, err
}
func (s *Store) finish(r Request, result Result) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, e := os.CreateTemp(s.dir, "receipt-")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	e = json.NewEncoder(f).Encode(receipt{Digest: digest(r), Result: result})
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e == nil {
		e = closeErr
	}
	if e != nil {
		return e
	}
	if e = os.Rename(name, filepath.Join(s.dir, r.CommandID+".json")); e != nil {
		return e
	}
	return s.syncDir()
}
func (s *Store) Execute(ctx context.Context, r Request, dispatch func(context.Context, Request) Result) Result {
	prior, owned, err := s.reserve(r)
	if err != nil {
		return Result{Status: "rejected", Code: "RECEIPT_STORAGE_FAILED"}
	}
	if !owned {
		return prior
	}
	result := dispatch(ctx, r)
	if result.Status != "accepted" && result.Status != "rejected" && result.Status != "delivery_unknown" {
		result = Result{Status: "delivery_unknown", Code: "INVALID_DELIVERY_RESULT"}
	}
	if s.finish(r, result) != nil {
		return Result{Status: "delivery_unknown", Code: "RECEIPT_STORAGE_FAILED"}
	}
	return result
}
