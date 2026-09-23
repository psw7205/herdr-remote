package stream

import (
	"fmt"
	"testing"
)

func TestSnapshotSubscribeGapAndLive(t *testing.T) {
	s := New("session")
	s.Append("message.user", map[string]string{"text": "one"})
	snap := s.Snapshot()
	s.Append("message.assistant", map[string]string{"text": "two"})
	replay, live, close := s.Subscribe(snap.Cursor)
	defer close()
	if len(replay) != 1 || replay[0].Cursor.Sequence != 2 {
		t.Fatalf("lost gap: %+v", replay)
	}
	s.Append("agent.status", map[string]string{"status": "idle"})
	if got := <-live; got.Cursor.Sequence != 3 {
		t.Fatalf("wrong live order: %+v", got)
	}
}
func TestOldEpochExpiredAndFutureCursorRequireSnapshot(t *testing.T) {
	s := New("session")
	old := s.Snapshot().Cursor
	s.Reset(map[string]string{"new": "snapshot"})
	for _, c := range []Cursor{old, {Epoch: s.Snapshot().Cursor.Epoch, Sequence: 99}} {
		r, _, stop := s.Subscribe(c)
		stop()
		if len(r) != 1 || r[0].Type != "session.snapshot" {
			t.Fatalf("not resynced %+v", r)
		}
	}
	expired := s.Snapshot().Cursor
	for i := 0; i < 1100; i++ {
		s.Append("message.user", fmt.Sprintf("%d", i))
	}
	r, _, stop := s.Subscribe(expired)
	defer stop()
	if r[0].Type != "session.snapshot" {
		t.Fatal("expired cursor replayed")
	}
}
func TestSlowSubscriberDisconnectsWithoutStoppingSession(t *testing.T) {
	s := New("session")
	_, ch, stop := s.Subscribe(s.Snapshot().Cursor)
	defer stop()
	for i := 0; i < 150; i++ {
		s.Append("agent.status", i)
	}
	n := 0
	for range ch {
		n++
	}
	if n != 128 {
		t.Fatalf("buffer size %d", n)
	}
	s.Append("agent.status", 200)
	if s.Snapshot().Cursor.Sequence != 151 {
		t.Fatal("stream stopped")
	}
}
