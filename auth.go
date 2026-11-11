package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Session is a browser login. It is persisted to data/sessions.json so a
// restart does not log everyone out — sessions live SessionDays (7 by default).
type Session struct {
	Token       string    `json:"token"`
	DisplayName string    `json:"display_name"`
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   time.Time `json:"expires_at"`
}

type SessionStore struct {
	mu       sync.Mutex
	path     string
	sessions map[string]*Session
	ttl      time.Duration
}

func NewSessionStore(dataDir string, ttl time.Duration) (*SessionStore, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	st := &SessionStore{
		path:     filepath.Join(dataDir, "sessions.json"),
		sessions: map[string]*Session{},
		ttl:      ttl,
	}
	data, err := os.ReadFile(st.path)
	if err != nil {
		if os.IsNotExist(err) {
			return st, nil
		}
		return nil, err
	}
	if len(data) > 0 {
		var list []*Session
		if err := json.Unmarshal(data, &list); err != nil {
			return nil, err
		}
		now := time.Now()
		for _, s := range list {
			if s != nil && s.ExpiresAt.After(now) {
				st.sessions[s.Token] = s
			}
		}
	}
	return st, nil
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (st *SessionStore) Create() (*Session, error) {
	tok, err := newToken()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	s := &Session{Token: tok, CreatedAt: now, ExpiresAt: now.Add(st.ttl)}
	st.mu.Lock()
	st.sessions[tok] = s
	err = st.saveLocked()
	st.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (st *SessionStore) Get(token string) *Session {
	if token == "" {
		return nil
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	s, ok := st.sessions[token]
	if !ok || s.ExpiresAt.Before(time.Now()) {
		return nil
	}
	return s
}

// SetName assigns the visitor's own display name. Names must be unique
// among currently-active sessions (case-insensitive), so two people
// cannot impersonate each other in the room.
func (st *SessionStore) SetName(token, name string) error {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 30 {
		return fmt.Errorf("display name must be 1–30 characters")
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	for tok, s := range st.sessions {
		if tok != token && s.ExpiresAt.After(time.Now()) &&
			strings.EqualFold(strings.TrimSpace(s.DisplayName), name) {
			return fmt.Errorf("that display name is already taken — pick another one")
		}
	}
	s, ok := st.sessions[token]
	if !ok {
		return fmt.Errorf("session not found — please log in again")
	}
	s.DisplayName = name
	return st.saveLocked()
}

func (st *SessionStore) Delete(token string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	delete(st.sessions, token)
	_ = st.saveLocked()
}

func (st *SessionStore) saveLocked() error {
	list := make([]*Session, 0, len(st.sessions))
	now := time.Now()
	for _, s := range st.sessions {
		if s.ExpiresAt.After(now) {
			list = append(list, s)
		}
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	tmp := st.path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, st.path)
}

// --- Maths captcha (addition / subtraction, 5-minute expiry) ---

type captcha struct {
	answer    int
	expiresAt time.Time
}

type CaptchaStore struct {
	mu    sync.Mutex
	items map[string]captcha
}

func NewCaptchaStore() *CaptchaStore {
	return &CaptchaStore{items: map[string]captcha{}}
}

func randInt(n int64) int {
	v, err := rand.Int(rand.Reader, big.NewInt(n))
	if err != nil {
		return 0
	}
	return int(v.Int64())
}

// New creates a captcha like "7 + 5 = ?" and returns its id + question.
func (c *CaptchaStore) New() (id, question string, err error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	id = hex.EncodeToString(b)
	a, b2 := randInt(20)+1, randInt(20)+1
	var answer int
	if randInt(2) == 0 {
		question = fmt.Sprintf("%d + %d = ؟", a, b2)
		answer = a + b2
	} else {
		if b2 > a {
			a, b2 = b2, a
		}
		question = fmt.Sprintf("%d - %d = ؟", a, b2)
		answer = a - b2
	}
	c.mu.Lock()
	c.items[id] = captcha{answer: answer, expiresAt: time.Now().Add(5 * time.Minute)}
	c.mu.Unlock()
	return id, question, nil
}

// Verify checks an answer once — a captcha cannot be replayed.
func (c *CaptchaStore) Verify(id string, answer int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	it, ok := c.items[id]
	delete(c.items, id)
	if !ok || time.Now().After(it.expiresAt) {
		return false
	}
	return it.answer == answer
}
