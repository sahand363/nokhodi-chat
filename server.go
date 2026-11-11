package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const sessionCookie = "nokhodi_session"
const nameCookie = "nokhodi_name"

type Server struct {
	cfg      *Config
	cfgPath  string
	store    *Store
	sessions *SessionStore
	captchas *CaptchaStore
	broker   *Broker
}

func NewServer(cfg *Config, cfgPath string) (*Server, error) {
	store, err := NewStore(cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("messages store: %w", err)
	}
	sessions, err := NewSessionStore(cfg.DataDir, time.Duration(cfg.SessionDays)*24*time.Hour)
	if err != nil {
		return nil, fmt.Errorf("sessions store: %w", err)
	}
	return &Server{
		cfg: cfg, cfgPath: cfgPath,
		store: store, sessions: sessions,
		captchas: NewCaptchaStore(), broker: NewBroker(),
	}, nil
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	// Static frontend (embedded in the binary — no files to deploy).
	web, _ := fs.Sub(webFS, "web")
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			// Let the file server handle /app.js and /style.css, 404 the rest.
			http.FileServer(http.FS(web)).ServeHTTP(w, r)
			return
		}
		data, err := webFS.ReadFile("web/index.html")
		if err != nil {
			http.Error(w, "frontend missing", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(data)
	})

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "ok", "title": s.cfg.Title,
			"messages": s.store.Count(), "online": s.broker.Count(),
		})
	})
	mux.HandleFunc("/api/captcha", s.handleCaptcha)
	mux.HandleFunc("/api/login", s.handleLogin)
	mux.HandleFunc("/api/identity", s.handleIdentity)
	mux.HandleFunc("/api/me", s.handleMe)
	mux.HandleFunc("/api/logout", s.handleLogout)
	mux.HandleFunc("/api/messages", s.handleMessages)
	mux.HandleFunc("/api/events", s.handleEvents)
	return mux
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// sessionFromRequest returns the logged-in session, if the cookie is valid.
func (s *Server) sessionFromRequest(r *http.Request) *Session {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return nil
	}
	return s.sessions.Get(c.Value)
}

func (s *Server) setSessionCookie(w http.ResponseWriter, sess *Session) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: sess.Token, Path: "/",
		HttpOnly: true, SameSite: http.SameSiteLaxMode,
		// Secure is false on purpose: the room is plain http:// on a LAN /
		// VPS. Set it to true only if you put TLS in front of it.
		MaxAge: s.cfg.SessionDays * 24 * 3600, Expires: sess.ExpiresAt,
	})
}

func (s *Server) handleCaptcha(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	id, q, err := s.captchas.New()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not create captcha")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id, "question": q})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if in.Username != s.cfg.Username || !checkPassword(s.cfg, in.Password) {
		// Same message for wrong user / wrong password — do not leak which.
		writeErr(w, http.StatusUnauthorized, "نام کاربری یا رمز عبور اشتباه است")
		return
	}
	sess, err := s.sessions.Create()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not create session")
		return
	}
	s.setSessionCookie(w, sess)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "needs_name": true})
}

func (s *Server) handleIdentity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	sess := s.sessionFromRequest(r)
	if sess == nil {
		writeErr(w, http.StatusUnauthorized, "please log in first")
		return
	}
	var in struct {
		DisplayName    string `json:"display_name"`
		CaptchaID      string `json:"captcha_id"`
		CaptchaAnswer  int    `json:"captcha_answer"`
		CaptchaAnswerS string `json:"captcha_answer_str"` // tolerate string answers
	}
	// Read the raw body once so we can accept the answer as number or string.
	body, err := readBody(r, 4096)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := json.Unmarshal(body, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if in.CaptchaAnswerS != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(in.CaptchaAnswerS)); err == nil {
			in.CaptchaAnswer = n
		}
	}
	if !s.captchas.Verify(in.CaptchaID, in.CaptchaAnswer) {
		writeErr(w, http.StatusBadRequest, "جواب کپچا اشتباه است — دوباره تلاش کنید")
		return
	}
	if err := s.sessions.SetName(sess.Token, in.DisplayName); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	name := strings.TrimSpace(in.DisplayName)
	// Plain (non-HttpOnly) cookie + localStorage so the browser remembers
	// the display name and pre-fills it next time.
	http.SetCookie(w, &http.Cookie{
		Name: nameCookie, Value: name, Path: "/",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   s.cfg.SessionDays * 24 * 3600, Expires: sess.ExpiresAt,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "display_name": name})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionFromRequest(r)
	if sess == nil {
		writeJSON(w, http.StatusOK, map[string]any{"authenticated": false, "has_name": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true, "has_name": sess.DisplayName != "",
		"display_name": sess.DisplayName, "title": s.cfg.Title,
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.sessions.Delete(c.Value)
	}
	for _, name := range []string{sessionCookie, nameCookie} {
		http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1})
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) requireIdentity(w http.ResponseWriter, r *http.Request) *Session {
	sess := s.sessionFromRequest(r)
	if sess == nil {
		writeErr(w, http.StatusUnauthorized, "please log in first")
		return nil
	}
	if strings.TrimSpace(sess.DisplayName) == "" {
		writeErr(w, http.StatusForbidden, "please pick a display name first")
		return nil
	}
	return sess
}

func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	sess := s.requireIdentity(w, r)
	if sess == nil {
		return
	}
	switch r.Method {
	case http.MethodGet:
		limit := 200
		if q := r.URL.Query().Get("limit"); q != "" {
			if n, err := strconv.Atoi(q); err == nil && n > 0 && n <= 1000 {
				limit = n
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"messages": s.store.Last(limit)})
	case http.MethodPost:
		var in struct {
			Text string `json:"text"`
		}
		if err := decodeJSON(r, &in); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		text := strings.TrimSpace(in.Text)
		if text == "" {
			writeErr(w, http.StatusBadRequest, "message is empty")
			return
		}
		if len([]rune(text)) > 1000 {
			writeErr(w, http.StatusBadRequest, "message is too long (max 1000 characters)")
			return
		}
		m, err := s.store.Add(sess.DisplayName, text)
		if err != nil {
			log.Printf("store message: %v", err)
			writeErr(w, http.StatusInternalServerError, "could not save message")
			return
		}
		s.broker.Publish(m)
		writeJSON(w, http.StatusCreated, m)
	default:
		writeErr(w, http.StatusMethodNotAllowed, "GET or POST only")
	}
}

// handleEvents is the SSE live feed: GET /api/events
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	sess := s.requireIdentity(w, r)
	if sess == nil {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming not supported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()

	ch := s.broker.Subscribe()
	defer s.broker.Unsubscribe(ch)
	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case m := <-ch:
			data, _ := json.Marshal(m)
			_, _ = fmt.Fprintf(w, "event: message\ndata: %s\n\n", data)
			flusher.Flush()
		case <-heartbeat.C:
			_, _ = fmt.Fprintf(w, ": heartbeat\n\n")
			flusher.Flush()
		}
	}
}

func readBody(r *http.Request, max int64) ([]byte, error) {
	defer r.Body.Close()
	// Small helper: read up to max+1 bytes so oversize bodies are rejected.
	buf := make([]byte, 0, 1024)
	tmp := make([]byte, 512)
	var total int64
	for {
		n, err := r.Body.Read(tmp)
		if n > 0 {
			total += int64(n)
			if total > max {
				return nil, fmt.Errorf("body too large")
			}
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			break
		}
	}
	return buf, nil
}
