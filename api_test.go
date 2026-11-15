package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestHealth(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/web/health", nil)
	response := httptest.NewRecorder()
	newServer(&fakeAuth{}, &bytes.Buffer{}, strings.NewReader(strings.Repeat("x", 64)), false).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("health status = %d, want %d", response.Code, http.StatusOK)
	}
}

type fakeAuth struct {
	ok        bool
	err       error
	requestID string
}

func (f *fakeAuth) authenticate(_ context.Context, _, _, requestID string) (bool, error) {
	f.requestID = requestID
	return f.ok, f.err
}

func TestLoginAndSession(t *testing.T) {
	auth := &fakeAuth{ok: true}
	var logs bytes.Buffer
	server := newServer(auth, &logs, strings.NewReader(strings.Repeat("x", 64)), false)
	login := httptest.NewRequest(http.MethodPost, "/api/web/login", strings.NewReader(`{"username":"alice","password":"secret"}`))
	response := httptest.NewRecorder()
	server.ServeHTTP(response, login)
	if response.Code != http.StatusOK || len(response.Result().Cookies()) != 1 {
		t.Fatalf("login failed: %d %s", response.Code, response.Body.String())
	}
	cookie := response.Result().Cookies()[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("unsafe session cookie")
	}
	if !strings.Contains(logs.String(), `"request_id":"`+auth.requestID+`"`) || strings.Contains(logs.String(), "secret") {
		t.Fatal("unsafe or uncorrelated API log")
	}
	request := httptest.NewRequest(http.MethodGet, "/api/web/session", nil)
	request.AddCookie(cookie)
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "alice") {
		t.Fatalf("session failed: %d %s", response.Code, response.Body.String())
	}
}

func TestLoginFailures(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		auth       fakeAuth
		random     string
		want       int
	}{
		{name: "invalid JSON", body: "{", want: http.StatusBadRequest},
		{name: "empty credentials", body: `{}`, want: http.StatusUnauthorized},
		{name: "wrong credentials", body: `{"username":"alice","password":"wrong"}`, want: http.StatusUnauthorized},
		{name: "database failure", body: `{"username":"alice","password":"secret"}`, auth: fakeAuth{err: errors.New("database failure")}, want: http.StatusInternalServerError},
		{name: "random failure", body: `{"username":"alice","password":"secret"}`, auth: fakeAuth{ok: true}, want: http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := newServer(&tc.auth, &bytes.Buffer{}, strings.NewReader(tc.random), false)
			response := httptest.NewRecorder()
			server.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/web/login", strings.NewReader(tc.body)))
			if response.Code != tc.want {
				t.Fatalf("status = %d, want %d", response.Code, tc.want)
			}
		})
	}
}

func TestProtectedSessionAndHome(t *testing.T) {
	server := newServer(&fakeAuth{}, &bytes.Buffer{}, strings.NewReader(""), true)
	for _, path := range []string{"/api/web/session", "/"} {
		response := httptest.NewRecorder()
		server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		want := http.StatusOK
		if path != "/" {
			want = http.StatusUnauthorized
		}
		if response.Code != want {
			t.Fatalf("%s: status %d, want %d", path, response.Code, want)
		}
	}
	request := httptest.NewRequest(http.MethodGet, "/api/web/session", nil)
	request.AddCookie(&http.Cookie{Name: "session", Value: "invalid"})
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatal("invalid session accepted")
	}
}

func TestExpiredSessions(t *testing.T) {
	server := newServer(&fakeAuth{ok: true}, &bytes.Buffer{}, strings.NewReader(strings.Repeat("x", 64)), true)
	server.sessions["expired"] = session{username: "alice", expires: time.Now().Add(-time.Hour)}
	request := httptest.NewRequest(http.MethodGet, "/api/web/session", nil)
	request.AddCookie(&http.Cookie{Name: "session", Value: "expired"})
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatal("expired session accepted")
	}
	server.sessions["expired"] = session{username: "alice", expires: time.Now().Add(-time.Hour)}
	server.sessions["active"] = session{username: "alice", expires: time.Now().Add(time.Hour)}
	response = httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/web/login", strings.NewReader(`{"username":"alice","password":"secret"}`)))
	if _, exists := server.sessions["expired"]; exists {
		t.Fatal("expired session was not removed")
	}
	if !response.Result().Cookies()[0].Secure {
		t.Fatal("secure cookie flag missing")
	}
}

func TestRun(t *testing.T) {
	for _, tc := range []struct{ name, fail string }{
		{name: "success"},
		{name: "missing credentials", fail: "config"},
		{name: "open failure", fail: "open"},
		{name: "bootstrap failure", fail: "bootstrap"},
		{name: "server failure", fail: "listen"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			if tc.fail != "config" && tc.fail != "open" {
				create := mock.ExpectExec("CREATE TABLE")
				if tc.fail == "bootstrap" {
					create.WillReturnError(errors.New("database error"))
				} else {
					create.WillReturnResult(sqlmock.NewResult(0, 0))
					mock.ExpectExec("INSERT INTO users").WithArgs("alice", sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
				}
				mock.ExpectClose()
			}
			values := map[string]string{"DB_HOST": "db:5432", "DB_USER": "app", "DB_PASSWORD": "db secret", "DB_NAME": "app", "INITIAL_USERNAME": "alice", "INITIAL_PASSWORD": "user secret"}
			if tc.fail == "config" {
				delete(values, "DB_PASSWORD")
			}
			open := func(_, _ string) (*sql.DB, error) {
				if tc.fail == "open" {
					return nil, errors.New("open error")
				}
				return conn, nil
			}
			listen := func(_ string, _ http.Handler) error {
				if tc.fail == "listen" {
					return errors.New("listen error")
				}
				return nil
			}
			var logs bytes.Buffer
			err = run(func(key string) string { return values[key] }, open, listen, &logs)
			if (err != nil) != (tc.fail != "") {
				t.Fatalf("run error = %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(logs.String(), "user secret") || strings.Contains(logs.String(), "db secret") {
				t.Fatal("secrets in logs")
			}
		})
	}
}

func TestMainFunction(t *testing.T) {
	originalStart, originalExit := start, exit
	defer func() { start, exit = originalStart, originalExit }()
	for _, fail := range []bool{false, true} {
		called := false
		start = func() error {
			if fail {
				return errors.New("do not log this")
			}
			return nil
		}
		exit = func(code int) {
			if code != 1 {
				t.Errorf("exit code = %d", code)
			}
			called = true
		}
		main()
		if called != fail {
			t.Fatalf("exit called = %v, want %v", called, fail)
		}
	}
}

func TestDefaultStartRequiresConfiguration(t *testing.T) {
	t.Setenv("DB_HOST", "")
	if err := start(); err == nil {
		t.Fatal("startup without configuration succeeded")
	}
}
