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
	fmt.Println("health, login, authenticated session, rejection and password hash verified")
}
