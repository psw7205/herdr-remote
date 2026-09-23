package transcript

import (
	"bytes"
	"errors"
	"io"
	"os"
)

const maxRecordBytes = 32 << 20
const readBatchBytes = 8 << 20

type Batch struct {
	Reset bool
	Lines [][]byte
	More  bool
}

// Tailer is owned by one session worker. It never writes the native transcript.
type Tailer struct {
	path    string
	info    os.FileInfo
	offset  int64
	partial []byte
	head    []byte
}

func NewTailer(path string) *Tailer { return &Tailer{path: path} }
func (t *Tailer) Read() (Batch, error) {
	f, err := os.Open(t.path)
	if err != nil {
		return Batch{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return Batch{}, err
	}
	if !info.Mode().IsRegular() {
		return Batch{}, errors.New("transcript is not a regular file")
	}
	// A prefix anchor also detects truncate-and-regrow between watcher callbacks.
	n := min(info.Size(), int64(len(t.head)))
	anchor := make([]byte, n)
	if n > 0 {
		if _, err = f.ReadAt(anchor, 0); err != nil {
			return Batch{}, err
		}
	}
	reset := t.info == nil || !os.SameFile(t.info, info) || info.Size() < t.offset || !bytes.Equal(anchor, t.head[:n])
	if !reset && info.Size() == t.offset && !info.ModTime().Equal(t.info.ModTime()) {
		reset = true
	}
	if reset {
		t.offset = 0
		t.partial = nil
		t.head = nil
	}
	if _, err = f.Seek(t.offset, io.SeekStart); err != nil {
		return Batch{}, err
	}
	data, err := io.ReadAll(io.LimitReader(f, readBatchBytes))
	if err != nil {
		return Batch{}, err
	}
	t.offset += int64(len(data))
	t.info = info
	if len(t.head) == 0 && len(data) > 0 {
		t.head = append([]byte(nil), data[:min(len(data), 4096)]...)
	}
	t.partial = append(t.partial, data...)
	result := Batch{Reset: reset, More: t.offset < info.Size()}
	for {
		at := bytes.IndexByte(t.partial, '\n')
		if at < 0 {
			break
		}
		if at > maxRecordBytes {
			return Batch{}, errors.New("transcript record exceeds size limit")
		}
		line := bytes.TrimSuffix(t.partial[:at], []byte{'\r'})
		if len(line) > 0 {
			result.Lines = append(result.Lines, bytes.Clone(line))
		}
		t.partial = t.partial[at+1:]
	}
	if len(t.partial) > maxRecordBytes {
		return Batch{}, errors.New("transcript record exceeds size limit")
	}
	t.partial = bytes.Clone(t.partial)
	return result, nil
}
