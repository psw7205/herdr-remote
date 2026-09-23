// Package stream owns the atomic snapshot/replay/live boundary for one session.
package stream

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

type Cursor struct {
	Epoch    string `json:"epoch"`
	Sequence uint64 `json:"sequence"`
}
type Event struct {
	ProtocolVersion int             `json:"protocol_version"`
	ID              string          `json:"event_id"`
	SessionID       string          `json:"session_id"`
	Cursor          Cursor          `json:"cursor"`
	Timestamp       string          `json:"timestamp"`
	Type            string          `json:"type"`
	Payload         json.RawMessage `json:"payload"`
	Source          string          `json:"source"`
}
type Snapshot struct {
	Cursor Cursor          `json:"cursor"`
	Data   json.RawMessage `json:"data"`
}
type Change struct {
	Type    string
	Payload any
	Source  string
}
type Session struct {
	mu          sync.Mutex
	id          string
	cursor      Cursor
	state       json.RawMessage
	ring        []Event
	bytes       int
	subscribers map[chan Event]struct{}
}

func New(id string) *Session {
	return &Session{id: id, cursor: Cursor{Epoch: rand.Text()}, state: json.RawMessage(`{}`), subscribers: make(map[chan Event]struct{})}
}
func (s *Session) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Snapshot{s.cursor, append(json.RawMessage(nil), s.state...)}
}
func (s *Session) Append(kind string, payload any) {
	_ = s.Commit(nil, []Change{{Type: kind, Payload: payload, Source: "bridge"}})
}

// Commit freezes the projection and publishes its changes under the same lock
// used by Snapshot and Subscribe. A client never sees a cursor ahead of state.
func (s *Session) Commit(state any, changes []Change) error {
	var raw json.RawMessage
	if state != nil {
		b, e := json.Marshal(state)
		if e != nil {
			return e
		}
		raw = b
	}
	events := make([]Event, len(changes))
	for i, c := range changes {
		b, e := json.Marshal(c.Payload)
		if e != nil {
			return e
		}
		events[i] = Event{Type: c.Type, Payload: b, Source: c.Source}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if raw != nil {
		s.state = raw
	}
	for _, event := range events {
		s.cursor.Sequence++
		event = s.envelope(event)
		s.ring = append(s.ring, event)
		s.bytes += len(event.Payload)
		for len(s.ring) > 1024 || s.bytes > 8<<20 {
			s.bytes -= len(s.ring[0].Payload)
			s.ring = s.ring[1:]
		}
		s.broadcast(event)
	}
	return nil
}
func (s *Session) envelope(e Event) Event {
	e.ProtocolVersion = 1
	e.SessionID = s.id
	e.Cursor = s.cursor
	e.ID = fmt.Sprintf("%s:%d", s.cursor.Epoch, s.cursor.Sequence)
	e.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	return e
}
func (s *Session) snapshotEvent() Event {
	b, _ := json.Marshal(Snapshot{s.cursor, s.state})
	return s.envelope(Event{Type: "session.snapshot", Payload: b, Source: "bridge"})
}
func (s *Session) Reset(state any) error {
	b, e := json.Marshal(state)
	if e != nil {
		return e
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = b
	s.cursor = Cursor{Epoch: rand.Text()}
	s.ring = nil
	s.bytes = 0
	s.broadcast(s.snapshotEvent())
	return nil
}
func (s *Session) broadcast(e Event) {
	for ch := range s.subscribers {
		select {
		case ch <- e:
		default:
			close(ch)
			delete(s.subscribers, ch)
		}
	}
}
func (s *Session) Subscribe(after Cursor) ([]Event, <-chan Event, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	replay := []Event{}
	valid := after.Epoch == s.cursor.Epoch && after.Sequence <= s.cursor.Sequence
	if valid && after.Sequence < s.cursor.Sequence {
		valid = len(s.ring) > 0 && after.Sequence+1 >= s.ring[0].Cursor.Sequence
	}
	if !valid {
		replay = append(replay, s.snapshotEvent())
	} else {
		for _, e := range s.ring {
			if e.Cursor.Sequence > after.Sequence {
				replay = append(replay, e)
			}
		}
	}
	ch := make(chan Event, 128)
	s.subscribers[ch] = struct{}{}
	var once sync.Once
	stop := func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			if _, ok := s.subscribers[ch]; ok {
				delete(s.subscribers, ch)
				close(ch)
			}
		})
	}
	return replay, ch, stop
}
