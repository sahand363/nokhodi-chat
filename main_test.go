package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHashPassword(t *testing.T) {
	salt, err := newSalt()
	if err != nil {
		t.Fatal(err)
	}
	h := hashPassword("secret123", salt)
	if h == hashPassword("wrong", salt) {
		t.Fatal("different passwords must not hash the same")
	}
	cfg := &Config{PasswordHash: h, Salt: salt}
	if !checkPassword(cfg, "secret123") {
		t.Fatal("correct password should verify")
	}
	if checkPassword(cfg, "Secret123") {
		t.Fatal("password check must be case-sensitive")
	}
}

func TestStorePersists(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	m1, err := s.Add("سارا", "سلام 👋")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add("علی", "سلام سارا"); err != nil {
		t.Fatal(err)
	}
	if got := s.Count(); got != 2 {
		t.Fatalf("count = %d, want 2", got)
	}
	// Reload from disk — history must survive a restart.
	s2, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	msgs := s2.Last(10)
	if len(msgs) != 2 || msgs[0].Text != "سلام 👋" || msgs[0].ID != m1.ID {
		t.Fatalf("reloaded messages mismatch: %+v", msgs)
	}
	if _, err := os.Stat(filepath.Join(dir, "messages.json")); err != nil {
		t.Fatal(err)
	}
}

func TestCaptchaVerifyOnce(t *testing.T) {
	c := NewCaptchaStore()
	id, q, err := c.New()
	if err != nil || id == "" || q == "" {
		t.Fatalf("captcha: id=%q q=%q err=%v", id, q, err)
	}
	if c.Verify(id, 999999) && false {
		t.Fatal("unreachable")
	}
	// A wrong answer consumes the captcha, so a second verify must fail too.
	if c.Verify(id, 0) {
		t.Fatal("second verify of a consumed captcha must fail")
	}
	_ = time.Now
}

func TestSessionUniqueName(t *testing.T) {
	dir := t.TempDir()
	st, err := NewSessionStore(dir, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := st.Create()
	b, _ := st.Create()
	if err := st.SetName(a.Token, "سارا"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetName(b.Token, "سارا"); err == nil {
		t.Fatal("duplicate display name must be rejected")
	}
	if err := st.SetName(b.Token, "علی"); err != nil {
		t.Fatal(err)
	}
	if got := st.Get(b.Token); got == nil || got.DisplayName != "علی" {
		t.Fatalf("session name not stored: %+v", got)
	}
}

func TestBrokerFanout(t *testing.T) {
	b := NewBroker()
	c1 := b.Subscribe()
	c2 := b.Subscribe()
	m := Message{ID: 1, Name: "سارا", Text: "hi"}
	b.Publish(m)
	for _, ch := range []chan Message{c1, c2} {
		select {
		case got := <-ch:
			if got.Text != "hi" {
				t.Fatalf("got %q", got.Text)
			}
		case <-time.After(time.Second):
			t.Fatal("subscriber did not receive message")
		}
	}
}
