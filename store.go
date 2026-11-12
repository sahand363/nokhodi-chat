package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Message is one chat line in one room. Stored per room in
// data/room_<id>.json so history survives restarts and new joiners
// can read everything before them (until it self-destructs).
type Message struct {
	ID        int64      `json:"id"`
	RoomID    string     `json:"room_id,omitempty"`
	Name      string     `json:"name"`
	Text      string     `json:"text"`
	CreatedAt time.Time  `json:"created_at"`
	Unix      int64      `json:"unix"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

func (m *Message) Expired(now time.Time) bool {
	return m.ExpiresAt != nil && now.After(*m.ExpiresAt)
}

type Store struct {
	mu       sync.Mutex
	dataDir  string
	messages map[string][]Message // roomID -> messages
	nextID   map[string]int64
}

func NewStore(dataDir string, roomIDs []string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{dataDir: dataDir, messages: map[string][]Message{}, nextID: map[string]int64{}}
	for _, id := range roomIDs {
		if err := s.loadRoom(id); err != nil {
			return nil, err
		}
	}
	// Migrate v1: a single data/messages.json becomes #general history.
	legacy := filepath.Join(dataDir, "messages.json")
	if data, err := os.ReadFile(legacy); err == nil && len(data) > 0 {
		var msgs []Message
		if err := json.Unmarshal(data, &msgs); err == nil && len(msgs) > 0 {
			if len(s.messages["general"]) == 0 {
				for _, m := range msgs {
					m.RoomID = "general"
					s.messages["general"] = append(s.messages["general"], m)
					if m.ID >= s.nextID["general"] {
						s.nextID["general"] = m.ID + 1
					}
				}
				_ = s.saveLocked("general")
			}
			_ = os.Rename(legacy, legacy+".v1.bak")
		}
	}
	return s, nil
}

func (s *Store) roomPath(roomID string) string {
	return filepath.Join(s.dataDir, "room_"+roomID+".json")
}

func (s *Store) loadRoom(roomID string) error {
	if _, ok := s.messages[roomID]; ok {
		return nil
	}
	s.messages[roomID] = []Message{}
	s.nextID[roomID] = 1
	data, err := os.ReadFile(s.roomPath(roomID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(data) == 0 {
		return nil
	}
	var msgs []Message
	if err := json.Unmarshal(data, &msgs); err != nil {
		return err
	}
	// Drop already-expired messages on load.
	now := time.Now()
	kept := []Message{}
	for _, m := range msgs {
		if !m.Expired(now) {
			kept = append(kept, m)
		}
		if m.ID >= s.nextID[roomID] {
			s.nextID[roomID] = m.ID + 1
		}
	}
	s.messages[roomID] = kept
	return nil
}

// EnsureRoom loads a room's file when a new room is created at runtime.
func (s *Store) EnsureRoom(roomID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadRoom(roomID)
}

func (s *Store) DeleteRoom(roomID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.messages, roomID)
	delete(s.nextID, roomID)
	_ = os.Remove(s.roomPath(roomID))
}

// Add validates, appends and persists a message. ttlSeconds 0 = never.
func (s *Store) Add(roomID, name, text string, ttlSeconds int64) (Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadRoom(roomID); err != nil {
		return Message{}, err
	}
	now := time.Now()
	m := Message{ID: s.nextID[roomID], RoomID: roomID, Name: name, Text: text, CreatedAt: now, Unix: now.Unix()}
	s.nextID[roomID]++
	if ttlSeconds > 0 {
		exp := now.Add(time.Duration(ttlSeconds) * time.Second)
		m.ExpiresAt = &exp
	}
	s.messages[roomID] = append(s.messages[roomID], m)
	if err := s.saveLocked(roomID); err != nil {
		s.messages[roomID] = s.messages[roomID][:len(s.messages[roomID])-1]
		s.nextID[roomID]--
		return Message{}, err
	}
	return m, nil
}

// Last returns up to n most recent, non-expired messages, oldest first.
func (s *Store) Last(roomID string, n int) []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.loadRoom(roomID)
	msgs := s.pruneLocked(roomID)
	if n <= 0 || n >= len(msgs) {
		out := make([]Message, len(msgs))
		copy(out, msgs)
		return out
	}
	out := make([]Message, n)
	copy(out, msgs[len(msgs)-n:])
	return out
}

// Delete removes one message. Returns true if it existed.
func (s *Store) Delete(roomID string, id int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	msgs := s.messages[roomID]
	for i, m := range msgs {
		if m.ID == id {
			s.messages[roomID] = append(msgs[:i], msgs[i+1:]...)
			_ = s.saveLocked(roomID)
			return true
		}
	}
	return false
}

func (s *Store) Get(roomID string, id int64) *Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.messages[roomID] {
		if m.ID == id {
			cp := m
			return &cp
		}
	}
	return nil
}

func (s *Store) Count(roomID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.pruneLocked(roomID))
}

func (s *Store) TotalCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for id := range s.messages {
		n += len(s.pruneLocked(id))
	}
	return n
}

// PruneExpired removes expired messages from every room. Returns how many.
func (s *Store) PruneExpired() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for id := range s.messages {
		before := len(s.messages[id])
		s.pruneLocked(id)
		n += before - len(s.messages[id])
		if before != len(s.messages[id]) {
			_ = s.saveLocked(id)
		}
	}
	return n
}

func (s *Store) pruneLocked(roomID string) []Message {
	now := time.Now()
	msgs := s.messages[roomID]
	kept := msgs[:0]
	for _, m := range msgs {
		if !m.Expired(now) {
			kept = append(kept, m)
		}
	}
	s.messages[roomID] = kept
	return kept
}

func (s *Store) saveLocked(roomID string) error {
	data, err := json.MarshalIndent(s.messages[roomID], "", "  ")
	if err != nil {
		return err
	}
	tmp := s.roomPath(roomID) + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.roomPath(roomID))
}
