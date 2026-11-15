package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
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
					mock.ExpectExec("CREATE TABLE IF NOT EXISTS tasks").WillReturnResult(sqlmock.NewResult(0, 0))
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

func TestValidateTask(t *testing.T) {
	if problems := validateTask(normalizeTask(task{Title: "  Write report ", Priority: "high", Status: "done", DueDate: "2026-11-20", Category: " work "})); len(problems) != 0 {
		t.Fatalf("valid task rejected: %v", problems)
	}
	problems := validateTask(task{Title: "ab", Description: strings.Repeat("d", 501), Priority: "urgent", Status: "later", DueDate: "20/11/2026", Category: "x"})
	for _, field := range []string{"title", "description", "priority", "status", "due_date", "category"} {
		if problems[field] == "" {
			t.Errorf("missing validation error for %s", field)
		}
	}
	if problems := validateTask(task{Title: strings.Repeat("é", 100), Priority: "low", Status: "pending", DueDate: "2026-02-28", Category: strings.Repeat("ç", 50)}); len(problems) != 0 {
		t.Fatalf("multibyte limits rejected: %v", problems)
	}
	if problems := validateTask(task{Title: strings.Repeat("t", 101), Priority: "low", Status: "pending", DueDate: "2026-02-30", Category: strings.Repeat("c", 51)}); len(problems) != 3 {
		t.Fatalf("upper limits and impossible date: %v", problems)
	}
}

type fakeTasks struct {
	saved     task
	id        int64
	err       error
	requestID string
	calls     int
}

func (f *fakeTasks) record(id int64, input task, requestID string) (task, error) {
	f.calls++
	f.id, f.saved, f.requestID = id, input, requestID
	if f.err != nil {
		return task{}, f.err
	}
	input.ID = id
	if id == 0 {
		input.ID = 42
	}
	return input, nil
}

func (f *fakeTasks) createWebTask(_ context.Context, input task, requestID string) (task, error) {
	return f.record(0, input, requestID)
}

func (f *fakeTasks) updateWebTask(_ context.Context, id int64, input task, requestID string) (task, error) {
	return f.record(id, input, requestID)
}

func (f *fakeTasks) getWebTask(_ context.Context, id int64, requestID string) (task, error) {
	return f.record(id, sampleTask, requestID)
}

func taskServer(store *fakeTasks) (*server, *bytes.Buffer) {
	var logs bytes.Buffer
	server := newServer(&fakeAuth{}, &logs, strings.NewReader(""), false)
	server.tasks = store
	server.sessions["valid"] = session{username: "alice", expires: time.Now().Add(time.Hour)}
	return server, &logs
}

func taskRequest(method, path, body string, authenticated bool) *http.Request {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if authenticated {
		request.AddCookie(&http.Cookie{Name: "session", Value: "valid"})
	}
	return request
}

const validTaskJSON = `{"title":" Write report ","description":"Quarterly numbers","priority":"high","status":"pending","due_date":"2026-11-20","category":"work"}`

func TestWebTaskEndpoints(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, operation string
		wantID                        int64
		wantStatus                    int
	}{
		{name: "create", method: http.MethodPost, path: "/api/web/records", operation: "create", wantID: 42, wantStatus: http.StatusCreated},
		{name: "update", method: http.MethodPut, path: "/api/web/records/7", operation: "update", wantID: 7, wantStatus: http.StatusOK},
		{name: "get", method: http.MethodGet, path: "/api/web/records/7", operation: "get", wantID: 7, wantStatus: http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeTasks{}
			server, logs := taskServer(store)
			response := httptest.NewRecorder()
			server.ServeHTTP(response, taskRequest(tc.method, tc.path, validTaskJSON, true))
			if response.Code != tc.wantStatus || response.Header().Get("Content-Type") != "application/json" {
				t.Fatalf("status = %d, body %s", response.Code, response.Body.String())
			}
			var got task
			if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil || got.ID != tc.wantID || got.Title != "Write report" {
				t.Fatalf("response task = %+v, %v", got, err)
			}
			if tc.method != http.MethodGet && store.saved.Title != "Write report" {
				t.Fatalf("payload was not normalized: %+v", store.saved)
			}
			want := `"layer":"api","client":"web","operation":"` + tc.operation + `","status":"success","request_id":"` + store.requestID + `"`
			if store.requestID == "" || !strings.Contains(logs.String(), want) {
				t.Fatalf("API log not correlated: %s", logs.String())
			}
		})
	}
}

func TestWebTaskEndpointFailures(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, body string
		authenticated            bool
		storeErr                 error
		want                     int
		wantErrors               bool
	}{
		{name: "create without session", method: http.MethodPost, path: "/api/web/records", body: validTaskJSON, want: http.StatusUnauthorized},
		{name: "update without session", method: http.MethodPut, path: "/api/web/records/7", body: validTaskJSON, want: http.StatusUnauthorized},
		{name: "get without session", method: http.MethodGet, path: "/api/web/records/7", want: http.StatusUnauthorized},
		{name: "invalid JSON", method: http.MethodPost, path: "/api/web/records", body: "{", authenticated: true, want: http.StatusBadRequest},
		{name: "unknown field", method: http.MethodPost, path: "/api/web/records", body: `{"owner":"bob"}`, authenticated: true, want: http.StatusBadRequest},
		{name: "validation errors", method: http.MethodPost, path: "/api/web/records", body: `{"title":"ab"}`, authenticated: true, want: http.StatusUnprocessableEntity, wantErrors: true},
		{name: "update missing task", method: http.MethodPut, path: "/api/web/records/7", body: validTaskJSON, authenticated: true, storeErr: errTaskNotFound, want: http.StatusNotFound},
		{name: "get missing task", method: http.MethodGet, path: "/api/web/records/7", authenticated: true, storeErr: errTaskNotFound, want: http.StatusNotFound},
		{name: "storage failure", method: http.MethodPost, path: "/api/web/records", body: validTaskJSON, authenticated: true, storeErr: errors.New("database down"), want: http.StatusInternalServerError},
		{name: "non numeric id", method: http.MethodPut, path: "/api/web/records/abc", body: validTaskJSON, authenticated: true, want: http.StatusNotFound},
		{name: "zero id", method: http.MethodGet, path: "/api/web/records/0", authenticated: true, want: http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeTasks{err: tc.storeErr}
			server, logs := taskServer(store)
			response := httptest.NewRecorder()
			server.ServeHTTP(response, taskRequest(tc.method, tc.path, tc.body, tc.authenticated))
			if response.Code != tc.want {
				t.Fatalf("status = %d, want %d (%s)", response.Code, tc.want, response.Body.String())
			}
			if (tc.storeErr == nil) != (store.calls == 0) {
				t.Fatalf("store calls = %d", store.calls)
			}
			if tc.wantErrors {
				var body struct{ Errors map[string]string }
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || len(body.Errors) != 5 {
					t.Fatalf("validation body = %s", response.Body.String())
				}
			}
			if !strings.Contains(logs.String(), `"layer":"api","client":"web"`) || !strings.Contains(logs.String(), `"status":"error"`) {
				t.Fatalf("failure not logged: %s", logs.String())
			}
		})
	}
}
