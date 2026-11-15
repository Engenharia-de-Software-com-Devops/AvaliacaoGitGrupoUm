package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestIntegration(t *testing.T) {
	base := os.Getenv("API_BASE_URL")
	if base == "" {
		t.Skip("integration service not selected")
	}
	client := &http.Client{Timeout: 2 * time.Second}
	var health *http.Response
	var err error
	for range 30 {
		health, err = client.Get(base + "/api/web/health")
		if err == nil {
			break
		}
		time.Sleep(time.Second)
	}
	if err != nil {
		t.Fatalf("healthcheck unavailable: %v", err)
	}
	health.Body.Close()
	if health.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d", health.StatusCode)
	}
	page, err := client.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	markup, err := io.ReadAll(page.Body)
	page.Body.Close()
	if err != nil || page.StatusCode != http.StatusOK || !strings.Contains(string(markup), `id="login"`) || !strings.Contains(string(markup), `id="password"`) {
		t.Fatal("login page unavailable")
	}
	unauthorized, err := client.Get(base + "/api/web/session")
	if err != nil {
		t.Fatal(err)
	}
	unauthorized.Body.Close()
	if unauthorized.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.StatusCode)
	}
	username, password := os.Getenv("INITIAL_USERNAME"), os.Getenv("INITIAL_PASSWORD")
	body, err := json.Marshal(map[string]string{"username": username, "password": password})
	if err != nil {
		t.Fatal(err)
	}
	login, err := client.Post(base+"/api/web/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, login.Body)
	login.Body.Close()
	if login.StatusCode != http.StatusOK || len(login.Cookies()) != 1 {
		t.Fatalf("login status = %d", login.StatusCode)
	}
	request, err := http.NewRequest(http.MethodGet, base+"/api/web/session", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(login.Cookies()[0])
	session, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	session.Body.Close()
	if session.StatusCode != http.StatusOK {
		t.Fatalf("session status = %d", session.StatusCode)
	}
	badBody, err := json.Marshal(map[string]string{"username": username, "password": password + "-wrong"})
	if err != nil {
		t.Fatal(err)
	}
	badLogin, err := client.Post(base+"/api/web/login", "application/json", bytes.NewReader(badBody))
	if err != nil {
		t.Fatal(err)
	}
	badLogin.Body.Close()
	if badLogin.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad login status = %d", badLogin.StatusCode)
	}
	connectionURL := url.URL{Scheme: "postgres", Host: os.Getenv("DB_HOST"), Path: os.Getenv("DB_NAME"), User: url.UserPassword(os.Getenv("DB_USER"), os.Getenv("DB_PASSWORD")), RawQuery: "sslmode=disable"}
	db, err := sql.Open("pgx", connectionURL.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var hash string
	if err := db.QueryRow(`SELECT password_hash FROM users WHERE username = $1`, username).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(hash, password) || bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		t.Fatal("password hash invalid")
	}
	cookie := login.Cookies()[0]
	call := func(method, path, body string, authenticated bool) (int, []byte) {
		request, err := http.NewRequest(method, base+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		if authenticated {
			request.AddCookie(cookie)
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		content, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		return response.StatusCode, content
	}
	newTask := `{"title":"Integration task","description":"Created by the integration test","priority":"high","status":"pending","due_date":"2026-11-20","category":"tests"}`
	if status, _ := call(http.MethodPost, "/api/web/records", newTask, false); status != http.StatusUnauthorized {
		t.Fatalf("unauthenticated create status = %d", status)
	}
	if status, body := call(http.MethodPost, "/api/web/records", `{"title":"x"}`, true); status != http.StatusUnprocessableEntity || !strings.Contains(string(body), "due_date") {
		t.Fatalf("invalid create status = %d, body %s", status, body)
	}
	status, body := call(http.MethodPost, "/api/web/records", newTask, true)
	var created struct {
		ID int64 `json:"id"`
	}
	if status != http.StatusCreated || json.Unmarshal(body, &created) != nil || created.ID < 1 {
		t.Fatalf("create status = %d, body %s", status, body)
	}
	recordPath := fmt.Sprintf("/api/web/records/%d", created.ID)
	edited := strings.Replace(strings.Replace(newTask, `"pending"`, `"done"`, 1), "Integration task", "Integration task edited", 1)
	if status, body := call(http.MethodPut, recordPath, edited, true); status != http.StatusOK || !strings.Contains(string(body), `"status":"done"`) {
		t.Fatalf("update status = %d, body %s", status, body)
	}
	if status, body := call(http.MethodGet, recordPath, "", true); status != http.StatusOK || !strings.Contains(string(body), "Integration task edited") {
		t.Fatalf("get status = %d, body %s", status, body)
	}
	if status, _ := call(http.MethodPut, "/api/web/records/999999999", edited, true); status != http.StatusNotFound {
		t.Fatalf("missing update status = %d", status)
	}
	var title, taskStatus, dueDate string
	if err := db.QueryRow(`SELECT title, status, to_char(due_date, 'YYYY-MM-DD') FROM tasks WHERE id = $1`, created.ID).Scan(&title, &taskStatus, &dueDate); err != nil {
		t.Fatal(err)
	}
	if title != "Integration task edited" || taskStatus != "done" || dueDate != "2026-11-20" {
		t.Fatalf("persisted task = %q %q %q", title, taskStatus, dueDate)
	}
	fmt.Println("health, login, authenticated session, rejection, password hash and task create/edit persistence verified")
}
