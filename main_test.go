package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHashPasswordAndRole(t *testing.T) {
	cfg := &Config{}
	if err := setPassword(cfg, "user", "user-secret"); err != nil {
		t.Fatal(err)
	}
	if err := setPassword(cfg, "op", "op-secret"); err != nil {
		t.Fatal(err)
	}
	if got := checkRole(cfg, "op-secret"); got != "op" {
		t.Fatalf("op password role = %q, want op", got)
	}
	if got := checkRole(cfg, "user-secret"); got != "user" {
		t.Fatalf("user password role = %q, want user", got)
	}
	if got := checkRole(cfg, "wrong"); got != "" {
		t.Fatalf("wrong password role = %q, want empty", got)
	}
	// Legacy v1 password still logs in as a user.
	legacy := &Config{PasswordHash: hashPassword("old123", "salt1"), Salt: "salt1"}
	if got := checkRole(legacy, "old123"); got != "user" {
		t.Fatalf("legacy password role = %q, want user", got)
	}
}

func TestStorePerRoomAndTTL(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir, []string{"general", "r2"})
	if err != nil {
		t.Fatal(err)
	}
	m1, err := s.Add("general", "سارا", "سلام 👋", TTLNever)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add("r2", "علی", "secret", TTL10Min); err != nil {
		t.Fatal(err)
	}
	if got := s.Count("general"); got != 1 {
		t.Fatalf("general count = %d, want 1", got)
	}
	if got := s.Count("r2"); got != 1 {
		t.Fatalf("r2 count = %d, want 1", got)
	}
	// A message with a TTL in the past must be pruned.
	past := time.Now().Add(-time.Minute)
	s.mu.Lock()
	s.messages["r2"] = append(s.messages["r2"], Message{ID: 99, RoomID: "r2", Name: "x", Text: "old", CreatedAt: past, ExpiresAt: &past})
	s.mu.Unlock()
	if n := s.PruneExpired(); n != 1 {
		t.Fatalf("pruned = %d, want 1", n)
	}
	// Reload from disk — history must survive a restart.
	s2, err := NewStore(dir, []string{"general", "r2"})
	if err != nil {
		t.Fatal(err)
	}
	msgs := s2.Last("general", 10)
	if len(msgs) != 1 || msgs[0].Text != "سلام 👋" || msgs[0].ID != m1.ID {
		t.Fatalf("reloaded messages mismatch: %+v", msgs)
	}
	if _, err := os.Stat(filepath.Join(dir, "room_general.json")); err != nil {
		t.Fatal(err)
	}
}

func TestRoomHiddenAndPassword(t *testing.T) {
	dir := t.TempDir()
	rs, err := NewRoomStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Default room exists and is visible.
	gen := rs.Get("general")
	if gen == nil || gen.Hidden {
		t.Fatal("general room should exist and be visible")
	}
	hidden, err := rs.Create("secret", "roompass", true, TTL24Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !hidden.Locked() || !hidden.Hidden {
		t.Fatal("hidden room should be locked and hidden")
	}
	user := &Session{Role: "user"}
	op := &Session{Role: "op"}
	// A normal user cannot even see the hidden room — no name, no 403 leak.
	if rs.CanSee(user, hidden, "") {
		t.Fatal("hidden room must not be visible to a normal user without invite")
	}
	if !rs.CanSee(user, hidden, hidden.InviteToken) {
		t.Fatal("hidden room must be visible with the correct invite token")
	}
	if !rs.CanSee(op, hidden, "") {
		t.Fatal("OP must see hidden rooms")
	}
	if !rs.CheckRoomPassword(hidden, "roompass") || rs.CheckRoomPassword(hidden, "wrong") {
		t.Fatal("room password check failed")
	}
	// Wrong TTL is rejected.
	if _, err := rs.Create("bad", "", false, 12345); err == nil {
		t.Fatal("invalid TTL should be rejected")
	}
}

func TestSessionUnlockAndBan(t *testing.T) {
	dir := t.TempDir()
	st, err := NewSessionStore(dir, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := st.Create("op")
	b, _ := st.Create("user")
	if !a.IsOP() || b.IsOP() {
		t.Fatal("roles not stored on sessions")
	}
	st.UnlockRoom(b.Token, "abc123")
	if !st.Get(b.Token).HasRoom("abc123") {
		t.Fatal("unlocked room not recorded")
	}
	if st.Get(a.Token).HasRoom("abc123") == false {
		// OP has access to every room by definition.
		t.Fatal("OP should have access to every room")
	}
	bans, err := NewBanStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	bans.Add("BadUser")
	if !bans.IsBanned("baduser") || bans.IsBanned("gooduser") {
		t.Fatal("ban check failed")
	}
}

func TestBrokerRoomIsolation(t *testing.T) {
	b := NewBroker()
	c1 := b.Subscribe("general")
	c2 := b.Subscribe("other")
	m := Message{ID: 1, Name: "سارا", Text: "hi"}
	b.Publish("general", Event{Type: "message", Message: &m})
	select {
	case got := <-c1:
		if got.Message == nil || got.Message.Text != "hi" {
			t.Fatalf("got %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("general subscriber did not receive message")
	}
	select {
	case got := <-c2:
		t.Fatalf("other room must not receive general messages, got %+v", got)
	case <-time.After(100 * time.Millisecond):
		// expected: no message
	}
}

func TestTLSConfigDefaultsOff(t *testing.T) {
	cfg := defaultConfig()
	if cfg.TLSEnabled || cfg.scheme() != "http" {
		t.Fatal("TLS must be off by default — plain HTTP keeps working")
	}
	cfg.TLSEnabled = true
	if cfg.scheme() != "https" {
		t.Fatal("scheme should be https when TLS is enabled")
	}
}

func TestUploadValidation(t *testing.T) {
	// Images, including GIFs, are allowed.
	if _, _, isImage, err := validateUpload("cat.gif", 1000, 10); err != nil || !isImage {
		t.Fatalf("gif should be allowed as image, err=%v", err)
	}
	if _, _, isImage, err := validateUpload("photo.PNG", 1000, 10); err != nil || !isImage {
		t.Fatalf("png should be allowed as image, err=%v", err)
	}
	// Ordinary files are allowed, executables are not.
	if _, _, isImage, err := validateUpload("notes.pdf", 1000, 10); err != nil || isImage {
		t.Fatalf("pdf should be allowed as a file, err=%v", err)
	}
	if _, _, _, err := validateUpload("virus.exe", 1000, 10); err == nil {
		t.Fatal("exe must be rejected")
	}
	if _, _, _, err := validateUpload("page.html", 1000, 10); err == nil {
		t.Fatal("html must be rejected")
	}
	if _, _, _, err := validateUpload("script.js", 1000, 10); err == nil {
		t.Fatal("js must be rejected")
	}
	// Size limit: 10 MB per file by default.
	if _, _, _, err := validateUpload("big.png", 11*1024*1024, 10); err == nil {
		t.Fatal("file over the size limit must be rejected")
	}
	if _, _, _, err := validateUpload("empty.png", 0, 10); err == nil {
		t.Fatal("empty file must be rejected")
	}
}

func TestUploadQuotaStats(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir, []string{"general"})
	if err != nil {
		t.Fatal(err)
	}
	att := &Attachment{ID: "abc123", FileName: "a.png", Size: 1024, Mime: "image/png", IsImage: true, Ext: ".png", URL: "/api/uploads/general/abc123.png"}
	if _, err := s.AddAttachment("general", "سارا", "", TTLNever, att); err != nil {
		t.Fatal(err)
	}
	count, bytes24h, total := s.AttachmentStats("سارا")
	if count != 1 || bytes24h != 1024 || total != 1024 {
		t.Fatalf("stats = %d, %d, %d — want 1, 1024, 1024", count, bytes24h, total)
	}
	// Deleting the message frees the quota.
	msgs := s.Last("general", 10)
	if !s.Delete("general", msgs[0].ID) {
		t.Fatal("delete failed")
	}
	count, _, total = s.AttachmentStats("سارا")
	if count != 0 || total != 0 {
		t.Fatalf("after delete stats = %d, %d — want 0, 0", count, total)
	}
}

func TestCaptchaVerifyOnce(t *testing.T) {
	c := NewCaptchaStore()
	id, q, err := c.New()
	if err != nil || id == "" || q == "" {
		t.Fatalf("captcha: id=%q q=%q err=%v", id, q, err)
	}
	if c.Verify(id, 0) {
		t.Fatal("second verify of a consumed captcha must fail")
	}
}
