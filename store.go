package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Message is one chat line. Stored in data/messages.json so history
// survives restarts and new joiners can read everything before them.
type Message struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
	Unix      int64     `json:"unix"`
}

type Store struct {
	mu       sync.Mutex
	path     string
	messages []Message
	nextID   int64
}

func NewStore(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{path: filepath.Join(dataDir, "messages.json"), nextID: 1}
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return s, nil
	}
	var msgs []Message
	if err := json.Unmarshal(data, &msgs); err != nil {
		return nil, err
	}
	s.messages = msgs
	for _, m := range msgs {
		if m.ID >= s.nextID {
			s.nextID = m.ID + 1
		}
	}
	return s, nil
}

// Add validates, appends and persists a message. Returns the stored copy.
func (s *Store) Add(name, text string) (Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	m := Message{ID: s.nextID, Name: name, Text: text, CreatedAt: now, Unix: now.Unix()}
	s.nextID++
	s.messages = append(s.messages, m)
	if err := s.saveLocked(); err != nil {
		// Roll back the in-memory append so memory and disk stay in sync.
		s.messages = s.messages[:len(s.messages)-1]
		s.nextID--
		return Message{}, err
	}
	return m, nil
}

// Last returns up to n most recent messages, oldest first.
func (s *Store) Last(n int) []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n <= 0 || n >= len(s.messages) {
		out := make([]Message, len(s.messages))
		copy(out, s.messages)
		return out
	}
	out := make([]Message, n)
	copy(out, s.messages[len(s.messages)-n:])
	return out
}

func (s *Store) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.messages)
}

func (s *Store) saveLocked() error {
	data, err := json.MarshalIndent(s.messages, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// All returns a copy sorted by ID (defensive; messages are already ordered).
func (s *Store) All() []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Message, len(s.messages))
	copy(out, s.messages)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
