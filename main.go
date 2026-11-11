// Command nokhodi-chat is a small, self-hosted Persian chat room.
//
//   - Single shared global login (username/password in config.json)
//   - Then each visitor picks their own display name + solves a maths captcha
//   - Messages are stored persistently in data/messages.json
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
	"strings"
)

//go:embed web
var webFS embed.FS

var version = "1.0.0"

// Config is the on-disk schema for config.json. The password is NEVER
// stored in plain text — only a salted SHA-256 hash.
type Config struct {
	Port         int    `json:"port"`
	Host         string `json:"host"`
	Username     string `json:"username"`
	PasswordHash string `json:"password_hash"`
	Salt         string `json:"salt"`
	SessionDays  int    `json:"session_days"`
	DataDir      string `json:"data_dir"`
	Title        string `json:"title"`
}

func defaultConfig() Config {
	return Config{
		Port:        44044,
		Host:        "0.0.0.0",
		Username:    "admin",
		SessionDays: 7,
		DataDir:     "data",
		Title:       "اتاق خواب نخودی",
	}
}

func newSalt() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func hashPassword(password, salt string) string {
	sum := sha256.Sum256([]byte(salt + ":" + password))
	return hex.EncodeToString(sum[:])
}

func checkPassword(cfg *Config, password string) bool {
	if cfg.PasswordHash == "" || cfg.Salt == "" {
		return false
	}
	return hashPassword(password, cfg.Salt) == cfg.PasswordHash
}

func loadConfig(path string) (*Config, error) {
	cfg := defaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// First run: create a config with a random salt and the
			// well-known default password. The operator MUST change it
			// via ./manage.sh -> option 3 (or `passwd` below).
			salt, err := newSalt()
			if err != nil {
				return nil, err
			}
			cfg.Salt = salt
			cfg.PasswordHash = hashPassword("changeme123", salt)
			if err := saveConfig(path, &cfg); err != nil {
				return nil, err
			}
			log.Printf("created %s with default login admin / changeme123 — change it with ./manage.sh (option 3)", path)
			return &cfg, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
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
		cfg.Title = "اتاق خواب نخودی"
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
	//   ./nokhodi-chat passwd -config config.json -user admin -pass 'new-secret'
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
	// Keep the config path on the server struct for the passwd flow.
	srv, err := NewServer(cfg, *configPath)
	if err != nil {
		log.Fatalf("server: %v", err)
	}

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	log.Printf("اتاق خواب نخودی — nokhodi-chat %s listening on http://%s", version, addr)
	log.Printf("open http://<server-ip>:%d  (default login admin / changeme123 — change it!)", cfg.Port)
	if err := http.ListenAndServe(addr, srv.routes()); err != nil {
		log.Fatal(err)
	}
}

// runPasswd implements `./nokhodi-chat passwd -user X -pass Y`.
// It is how manage.sh changes the shared global credentials safely:
// it re-hashes with a fresh salt and writes only the hash to config.json.
func runPasswd(configPath string) error {
	fs := flag.NewFlagSet("passwd", flag.ExitOnError)
	user := fs.String("user", "", "new shared username")
	pass := fs.String("pass", "", "new shared password")
	cfgPath := fs.String("config", configPath, "path to config.json")
	_ = fs.Parse(os.Args[2:])

	if *user == "" || *pass == "" {
		return fmt.Errorf("usage: nokhodi-chat passwd -user <name> -pass <password> [-config config.json]")
	}
	if len(*pass) < 6 {
		return fmt.Errorf("password must be at least 6 characters")
	}
	cfg, err := loadConfig(*cfgPath)
	if err != nil {
		return err
	}
	salt, err := newSalt()
	if err != nil {
		return err
	}
	cfg.Username = strings.TrimSpace(*user)
	cfg.Salt = salt
	cfg.PasswordHash = hashPassword(*pass, salt)
	if err := saveConfig(*cfgPath, cfg); err != nil {
		return err
	}
	fmt.Printf("credentials updated in %s (user %q, password stored as salted hash only)\n", *cfgPath, cfg.Username)
	return nil
}
