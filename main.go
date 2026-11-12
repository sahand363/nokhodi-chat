// Command nokhodi-chat is a small, self-hosted Persian chat room — v2.
//
//   - Two entry passwords: OP (admin) and everyone else (user).
//     The password you log in with decides your role; only the OP can
//     create rooms, set room passwords, set self-destruct timers,
//     delete any message, or kick/ban.
//   - mIRC-style rooms: #general plus rooms the OP creates. A room can
//     have its own password and a self-destruct timer, and it can be
//     hidden: hidden rooms are not listed, are only reachable via a
//     secret link, still need the room password, and return 404 —
//     no name, no messages — to anyone without link + password.
//   - Then each visitor picks their own display name + solves a maths captcha
//   - Messages are stored persistently per room in data/room_<id>.json
//   - Live updates use Server-Sent Events (SSE) — no external Go deps
//   - Serves on 0.0.0.0:44044 by default: http://<server-ip>:44044
//
// Standard library only, so `go build` works offline with no modules to fetch.
package main

import (
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
)

//go:embed web
var webFS embed.FS

var version = "2.0.1"

// Config is the on-disk schema for config.json. Passwords are NEVER
// stored in plain text — only salted SHA-256 hashes.
// Legacy fields PasswordHash/Salt are the v1 shared password; on load
// they are migrated to the user (everyone) password.
type Config struct {
	Port         int    `json:"port"`
	Host         string `json:"host"`
	Username     string `json:"username,omitempty"`
	PasswordHash string `json:"password_hash,omitempty"`
	Salt         string `json:"salt,omitempty"`
	// v2: two entry passwords.
	UserPasswordHash  string `json:"user_password_hash,omitempty"`
	UserSalt          string `json:"user_salt,omitempty"`
	AdminPasswordHash string `json:"admin_password_hash,omitempty"`
	AdminSalt         string `json:"admin_salt,omitempty"`
	SessionDays       int    `json:"session_days"`
	DataDir           string `json:"data_dir"`
	Title             string `json:"title"`
}

func defaultConfig() Config {
	return Config{
		Port:        44044,
		Host:        "0.0.0.0",
		SessionDays: 7,
		DataDir:     "data",
		Title:       "اتاق نخودی",
	}
}

func newSalt() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func newTokenHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func hashPassword(password, salt string) string {
	sum := sha256.Sum256([]byte(salt + ":" + password))
	return hex.EncodeToString(sum[:])
}

func hashMatches(password, salt, hash string) bool {
	if hash == "" || salt == "" || password == "" {
		return false
	}
	return hashPassword(password, salt) == hash
}

// checkRole returns "op" for the OP password, "user" for the
// everyone-else password, "" for neither. OP is checked first so the
// two passwords must be different to be useful.
func checkRole(cfg *Config, password string) string {
	if hashMatches(password, cfg.AdminSalt, cfg.AdminPasswordHash) {
		return "op"
	}
	if hashMatches(password, cfg.UserSalt, cfg.UserPasswordHash) {
		return "user"
	}
	// Legacy v1 fallback: the old shared password is the user password.
	if hashMatches(password, cfg.Salt, cfg.PasswordHash) {
		return "user"
	}
	return ""
}

func setPassword(cfg *Config, role, password string) error {
	salt, err := newSalt()
	if err != nil {
		return err
	}
	h := hashPassword(password, salt)
	switch role {
	case "op", "admin":
		cfg.AdminSalt, cfg.AdminPasswordHash = salt, h
	case "user":
		cfg.UserSalt, cfg.UserPasswordHash = salt, h
		// Keep the legacy fields in sync so old tooling still sees a password.
		cfg.Salt, cfg.PasswordHash = salt, h
	default:
		return fmt.Errorf("unknown role %q (want op or user)", role)
	}
	return nil
}

func loadConfig(path string) (*Config, error) {
	cfg := defaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// First run: user password changeme123, OP password changeme-op123.
			// The operator MUST change both via `nokhodichat` menu 3 and 4.
			if err := setPassword(&cfg, "user", "changeme123"); err != nil {
				return nil, err
			}
			if err := setPassword(&cfg, "op", "changeme-op123"); err != nil {
				return nil, err
			}
			if err := saveConfig(path, &cfg); err != nil {
				return nil, err
			}
			log.Printf("created %s — defaults: user changeme123, OP changeme-op123 — change both with nokhodichat (options 3 and 4)", path)
			return &cfg, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	// Migrate v1: old shared password becomes the user password.
	if cfg.UserPasswordHash == "" && cfg.PasswordHash != "" {
		cfg.UserPasswordHash, cfg.UserSalt = cfg.PasswordHash, cfg.Salt
	}
	// An existing v1 install has no OP password yet — give it the
	// well-known default so the OP can log in and change it at once.
	// (A fresh v2 install already has one from the branch above.)
	if cfg.AdminPasswordHash == "" {
		if err := setPassword(&cfg, "op", "changeme-op123"); err == nil {
			_ = saveConfig(path, &cfg)
			log.Printf("migrated %s: OP password set to default changeme-op123 — change it with nokhodichat (option 4)", path)
		}
	}
	if cfg.Port == 0 {
		cfg.Port = 44044
	}
	if cfg.Host == "" {
		cfg.Host = "0.0.0.0"
	}
	if cfg.SessionDays <= 0 {
		cfg.SessionDays = 7
	}
	if cfg.DataDir == "" {
		cfg.DataDir = "data"
	}
	if cfg.Title == "" {
		cfg.Title = "اتاق نخودی"
	}
	return &cfg, nil
}

func saveConfig(path string, cfg *Config) error {
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	// 0600: credentials live in this file.
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func main() {
	configPath := flag.String("config", "config.json", "path to config.json")
	flag.Parse()

	// Small CLI helpers used by manage.sh, e.g.:
	//   ./nokhodi-chat passwd -role op -pass 'new-op-secret'
	//   ./nokhodi-chat passwd -role user -pass 'new-user-secret'
	if len(flag.Args()) > 0 {
		switch flag.Arg(0) {
		case "version":
			fmt.Printf("nokhodi-chat %s\n", version)
			return
		case "passwd":
			if err := runPasswd(*configPath); err != nil {
				log.Fatal(err)
			}
			return
		}
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	srv, err := NewServer(cfg, *configPath)
	if err != nil {
		log.Fatalf("server: %v", err)
	}

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	log.Printf("اتاق نخودی — nokhodi-chat %s listening on http://%s", version, addr)
	log.Printf("open http://<server-ip>:%d  (user changeme123 / OP changeme-op123 by default — change both!)", cfg.Port)
	if err := http.ListenAndServe(addr, srv.routes()); err != nil {
		log.Fatal(err)
	}
}

// runPasswd implements `./nokhodi-chat passwd -role op|user -pass Y`.
// It is how manage.sh changes the two entry passwords safely:
// it re-hashes with a fresh salt and writes only the hash to config.json.
func runPasswd(configPath string) error {
	fs := flag.NewFlagSet("passwd", flag.ExitOnError)
	role := fs.String("role", "user", "which password to set: op or user")
	pass := fs.String("pass", "", "new password")
	cfgPath := fs.String("config", configPath, "path to config.json")
	_ = fs.Parse(os.Args[2:])

	if *pass == "" {
		return fmt.Errorf("usage: nokhodi-chat passwd -role op|user -pass <password> [-config config.json]")
	}
	if len(*pass) < 6 {
		return fmt.Errorf("password must be at least 6 characters")
	}
	cfg, err := loadConfig(*cfgPath)
	if err != nil {
		return err
	}
	if err := setPassword(cfg, *role, *pass); err != nil {
		return err
	}
	if err := saveConfig(*cfgPath, cfg); err != nil {
		return err
	}
	fmt.Printf("%s password updated in %s (stored as salted hash only)\n", *role, *cfgPath)
	return nil
}
