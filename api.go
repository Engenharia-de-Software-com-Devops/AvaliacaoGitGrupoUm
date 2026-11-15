package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	_ "github.com/jackc/pgx/v5/stdlib"
)

//go:embed index.html
var indexHTML string

type authenticator interface {
	authenticate(context.Context, string, string, string) (bool, error)
}

type session struct {
	username string
	expires  time.Time
}

type server struct {
	auth     authenticator
	logs     io.Writer
	random   io.Reader
	secure   bool
	mu       sync.Mutex
	sessions map[string]session
	sequence atomic.Uint64
}

func newServer(auth authenticator, logs io.Writer, random io.Reader, secure bool) *server {
	return &server{auth: auth, logs: logs, random: random, secure: secure, sessions: make(map[string]session)}
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id := strconv.FormatInt(time.Now().UnixNano(), 36) + "-" + strconv.FormatUint(s.sequence.Add(1), 36)
	status := http.StatusNotFound
	operation := "unknown"
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/web/health":
		operation, status = "health", http.StatusOK
	case r.Method == http.MethodPost && r.URL.Path == "/api/web/login":
		operation = "login"
		status = s.login(w, r, id)
	case r.Method == http.MethodGet && r.URL.Path == "/api/web/session":
		operation = "session"
		status = s.session(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/":
		operation, status = "home", http.StatusOK
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, indexHTML)
	}
	if operation == "health" || operation == "unknown" {
		w.WriteHeader(status)
	}
	result := "success"
	if status >= 400 {
		result = "error"
	}
	logEvent(s.logs, "api", operation, result, id, "Web request completed")
}

func (s *server) login(w http.ResponseWriter, r *http.Request, id string) int {
	var credentials struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if json.NewDecoder(r.Body).Decode(&credentials) != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return http.StatusBadRequest
	}
	if credentials.Username == "" || credentials.Password == "" {
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return http.StatusUnauthorized
	}
	ok, err := s.auth.authenticate(r.Context(), credentials.Username, credentials.Password, id)
	if err != nil {
		http.Error(w, "Authentication unavailable", http.StatusInternalServerError)
		return http.StatusInternalServerError
	}
	if !ok {
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return http.StatusUnauthorized
	}
	token := make([]byte, 32)
	if _, err := io.ReadFull(s.random, token); err != nil {
		http.Error(w, "Authentication unavailable", http.StatusInternalServerError)
		return http.StatusInternalServerError
	}
	value := hex.EncodeToString(token)
	expires := time.Now().Add(12 * time.Hour)
	s.mu.Lock()
	for key, existing := range s.sessions {
		if time.Now().After(existing.expires) {
			delete(s.sessions, key)
		}
	}
	s.sessions[value] = session{username: credentials.Username, expires: expires}
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "session", Value: value, Path: "/", HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteStrictMode, Expires: expires})
	w.WriteHeader(http.StatusOK)
	return http.StatusOK
}

func (s *server) session(w http.ResponseWriter, r *http.Request) int {
	cookie, err := r.Cookie("session")
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return http.StatusUnauthorized
	}
	s.mu.Lock()
	user, ok := s.sessions[cookie.Value]
	if ok && time.Now().After(user.expires) {
		delete(s.sessions, cookie.Value)
		ok = false
	}
	s.mu.Unlock()
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return http.StatusUnauthorized
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"username": user.username})
	return http.StatusOK
}

func run(getenv func(string) string, open func(string, string) (*sql.DB, error), listen func(string, http.Handler) error, logs io.Writer) error {
	host, user, password, name := getenv("DB_HOST"), getenv("DB_USER"), getenv("DB_PASSWORD"), getenv("DB_NAME")
	username, initialPassword := getenv("INITIAL_USERNAME"), getenv("INITIAL_PASSWORD")
	if host == "" || user == "" || password == "" || name == "" || username == "" || initialPassword == "" {
		return errors.New("database and initial user configuration are required")
	}
	connectionURL := url.URL{Scheme: "postgres", Host: host, Path: name, User: url.UserPassword(user, password)}
	query := connectionURL.Query()
	query.Set("sslmode", "disable")
	connectionURL.RawQuery = query.Encode()
	conn, err := open("pgx", connectionURL.String())
	if err != nil {
		return err
	}
	defer conn.Close()
	db := &database{conn: conn, logs: logs}
	if err := db.bootstrap(context.Background(), username, initialPassword, "startup"); err != nil {
		return err
	}
	return listen(":8080", newServer(db, logs, rand.Reader, getenv("COOKIE_SECURE") == "true"))
}

var start = func() error { return run(os.Getenv, sql.Open, http.ListenAndServe, os.Stdout) }
var exit = os.Exit

func main() {
	if err := start(); err != nil {
		logEvent(os.Stdout, "api", "startup", "error", "startup", "Application could not start")
		exit(1)
	}
}

var taskPriorities = map[string]bool{"low": true, "medium": true, "high": true}
var taskStatuses = map[string]bool{"pending": true, "in_progress": true, "done": true}

// normalizeTask trims surrounding whitespace from every text field.
func normalizeTask(input task) task {
	input.Title = strings.TrimSpace(input.Title)
	input.Description = strings.TrimSpace(input.Description)
	input.Priority = strings.TrimSpace(input.Priority)
	input.Status = strings.TrimSpace(input.Status)
	input.DueDate = strings.TrimSpace(input.DueDate)
	input.Category = strings.TrimSpace(input.Category)
	return input
}

// validateTask returns one user-facing message per invalid field.
func validateTask(input task) map[string]string {
	problems := map[string]string{}
	if length := utf8.RuneCountInString(input.Title); length < 3 || length > 100 {
		problems["title"] = "Informe um título com 3 a 100 caracteres."
	}
	if utf8.RuneCountInString(input.Description) > 500 {
		problems["description"] = "A descrição deve ter no máximo 500 caracteres."
	}
	if !taskPriorities[input.Priority] {
		problems["priority"] = "Escolha a prioridade: baixa, média ou alta."
	}
	if !taskStatuses[input.Status] {
		problems["status"] = "Escolha a situação: pendente, em andamento ou concluída."
	}
	if _, err := time.Parse(time.DateOnly, input.DueDate); err != nil {
		problems["due_date"] = "Informe uma data de entrega válida (AAAA-MM-DD)."
	}
	if length := utf8.RuneCountInString(input.Category); length < 2 || length > 50 {
		problems["category"] = "Informe uma categoria com 2 a 50 caracteres."
	}
	return problems
}
