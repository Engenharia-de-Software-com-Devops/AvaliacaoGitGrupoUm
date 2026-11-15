package main

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCommands(t *testing.T) {
	for _, tc := range []struct {
		name       string
		args       []string
		env        map[string]string
		status     int
		body       string
		failure    bool
		wantCode   int
		wantOutput string
	}{
		{name: "no args", wantOutput: "Usage:"},
		{name: "help", args: []string{"help"}, wantOutput: "Usage:"},
		{name: "short help", args: []string{"-h"}, wantOutput: "Usage:"},
		{name: "long help", args: []string{"--help"}, wantOutput: "Usage:"},
		{name: "unknown", args: []string{"unknown"}, wantCode: 2},
		{name: "extra args", args: []string{"health", "extra"}, wantCode: 2},
		{name: "health", args: []string{"health"}, status: 200, wantOutput: "OK"},
		{name: "credentials missing", args: []string{"token"}, wantCode: 2},
		{name: "password missing", args: []string{"token"}, env: map[string]string{"TODO_USERNAME": "alice"}, wantCode: 2},
		{name: "token", args: []string{"token"}, env: map[string]string{"TODO_USERNAME": "alice", "TODO_PASSWORD": "secret"}, status: 200, body: `{"token":"value"}`, wantOutput: "value"},
		{name: "bad token JSON", args: []string{"token"}, env: map[string]string{"TODO_USERNAME": "alice", "TODO_PASSWORD": "secret"}, status: 200, body: `{`, wantCode: 1},
		{name: "empty token", args: []string{"token"}, env: map[string]string{"TODO_USERNAME": "alice", "TODO_PASSWORD": "secret"}, status: 200, body: `{}`, wantCode: 1},
		{name: "missing bearer", args: []string{"session"}, wantCode: 2},
		{name: "session", args: []string{"session"}, env: map[string]string{"TODO_TOKEN": "value"}, status: 200, body: `{"username":"alice"}`, wantOutput: "alice"},
		{name: "bad session JSON", args: []string{"session"}, env: map[string]string{"TODO_TOKEN": "value"}, status: 200, body: `{`, wantCode: 1},
		{name: "empty username", args: []string{"session"}, env: map[string]string{"TODO_TOKEN": "value"}, status: 200, body: `{}`, wantCode: 1},
		{name: "unauthorized", args: []string{"session"}, env: map[string]string{"TODO_TOKEN": "value"}, status: 401, wantCode: 1},
		{name: "network failure", args: []string{"health"}, failure: true, wantCode: 1},
		{name: "bad URL", args: []string{"health"}, env: map[string]string{"TODO_API_URL": "://bad"}, wantCode: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
				if tc.failure {
					return nil, errors.New("network failure")
				}
				if r.URL.Host != "app:8080" {
					t.Fatalf("host = %q", r.URL.Host)
				}
				switch tc.args[0] {
				case "health":
					if r.URL.Path != "/api/cli/health" || r.Method != http.MethodGet {
						t.Fatal("wrong health request")
					}
				case "token":
					if r.URL.Path != "/api/cli/token" || r.Method != http.MethodPost {
						t.Fatal("wrong token request")
					}
					data, _ := io.ReadAll(r.Body)
					if !strings.Contains(string(data), `"username":"alice"`) || !strings.Contains(string(data), `"password":"secret"`) {
						t.Fatal("wrong token body")
					}
				case "session":
					if r.URL.Path != "/api/cli/session" || r.Header.Get("Authorization") != "Bearer value" {
						t.Fatal("wrong session request")
					}
				}
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body)), Header: make(http.Header)}, nil
			})}
			var stdout, stderr bytes.Buffer
			code := run(tc.args, func(k string) string { return tc.env[k] }, client, &stdout, &stderr)
			if code != tc.wantCode || !strings.Contains(stdout.String(), tc.wantOutput) {
				t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
			}
			if strings.Contains(stderr.String(), "secret") || strings.Contains(stderr.String(), "value") {
				t.Fatalf("secret in error output: %s", stderr.String())
			}
		})
	}
}

func TestMainFunction(t *testing.T) {
	originalInvoke, originalExit := invoke, exit
	defer func() { invoke, exit = originalInvoke, originalExit }()
	for _, status := range []int{0, 1} {
		called := false
		invoke = func() int { return status }
		exit = func(code int) {
			called = true
			if code != status {
				t.Fatalf("exit code = %d", code)
			}
		}
		main()
		if called != (status != 0) {
			t.Fatalf("exit called = %v", called)
		}
	}
}

func TestDefaultInvoke(t *testing.T) {
	old := os.Args
	defer func() { os.Args = old }()
	os.Args = []string{"cli", "--help"}
	if code := invoke(); code != 0 {
		t.Fatalf("default invoke = %d", code)
	}
}

func TestTaskCommands(t *testing.T) {
	createArgs := []string{"create", "--title", "Write report", "--priority", "high", "--due-date", "2026-11-20", "--category", "work"}
	updateArgs := []string{"update", "7", "--title", "Write report", "--description", "Quarterly numbers", "--priority", "high", "--status", "done", "--due-date", "2026-11-20", "--category", "work"}
	saved := `{"id":42,"title":"Write report","description":"","priority":"high","status":"pending","due_date":"2026-11-20","category":"work"}`
	token := map[string]string{"TODO_TOKEN": "cli-secret-token"}
	for _, tc := range []struct {
		name                 string
		args                 []string
		env                  map[string]string
		status               int
		body                 string
		failure              bool
		wantMethod, wantPath string
		wantBody             string
		wantCode             int
		wantStdout           string
		wantStderr           string
	}{
		{name: "create", args: createArgs, env: token, status: 201, body: saved, wantMethod: http.MethodPost, wantPath: "/api/cli/records",
			wantBody: `{"title":"Write report","description":"","priority":"high","status":"pending","due_date":"2026-11-20","category":"work"}`, wantStdout: "Task 42 created.\nid: 42\ntitle: Write report\ndescription: \npriority: high\nstatus: pending\ndue_date: 2026-11-20\ncategory: work\n"},
		{name: "update", args: updateArgs, env: token, status: 200, body: strings.Replace(saved, `"id":42`, `"id":7`, 1), wantMethod: http.MethodPut, wantPath: "/api/cli/records/7",
			wantBody: `{"title":"Write report","description":"Quarterly numbers","priority":"high","status":"done","due_date":"2026-11-20","category":"work"}`, wantStdout: "Task 7 updated.\nid: 7\n"},
		{name: "missing token", args: createArgs, wantCode: 2, wantStderr: "Set TODO_TOKEN."},
		{name: "update without id", args: []string{"update"}, env: token, wantCode: 2, wantStderr: "Usage:"},
		{name: "update with text id", args: []string{"update", "abc", "--title", "x"}, env: token, wantCode: 2, wantStderr: "positive integer"},
		{name: "update with zero id", args: []string{"update", "0"}, env: token, wantCode: 2, wantStderr: "positive integer"},
		{name: "unknown flag", args: []string{"create", "--owner", "bob"}, env: token, wantCode: 2, wantStderr: "Usage:"},
		{name: "extra argument", args: []string{"create", "--title", "x", "extra"}, env: token, wantCode: 2, wantStderr: "Usage:"},
		{name: "validation errors", args: createArgs, env: token, status: 422, body: `{"errors":{"title":"Informe um título.","category":"Informe uma categoria."}}`, wantMethod: http.MethodPost, wantPath: "/api/cli/records",
			wantCode: 1, wantStderr: "Validation failed:\n  category: Informe uma categoria.\n  title: Informe um título.\n"},
		{name: "invalid validation response", args: createArgs, env: token, status: 422, body: `{"errors":{}}`, wantMethod: http.MethodPost, wantPath: "/api/cli/records", wantCode: 1, wantStderr: "Invalid API response."},
		{name: "missing task", args: updateArgs, env: token, status: 404, wantMethod: http.MethodPut, wantPath: "/api/cli/records/7", wantCode: 1, wantStderr: "Task 7 not found."},
		{name: "unauthorized", args: createArgs, env: token, status: 401, wantMethod: http.MethodPost, wantPath: "/api/cli/records", wantCode: 1, wantStderr: "API returned HTTP 401."},
		{name: "invalid saved response", args: createArgs, env: token, status: 201, body: `{`, wantMethod: http.MethodPost, wantPath: "/api/cli/records", wantCode: 1, wantStderr: "Invalid API response."},
		{name: "network failure", args: createArgs, env: token, failure: true, wantCode: 1, wantStderr: "API connection failed."},
		{name: "bad URL", args: createArgs, env: map[string]string{"TODO_TOKEN": "cli-secret-token", "TODO_API_URL": "://bad"}, wantCode: 1, wantStderr: "Invalid API URL."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
				if tc.failure {
					return nil, errors.New("network failure")
				}
				data, _ := io.ReadAll(r.Body)
				if r.Method != tc.wantMethod || r.URL.Host != "app:8080" || r.URL.Path != tc.wantPath {
					t.Fatalf("request = %s %s", r.Method, r.URL)
				}
				if r.Header.Get("Authorization") != "Bearer cli-secret-token" || r.Header.Get("Content-Type") != "application/json" {
					t.Fatalf("headers = %v", r.Header)
				}
				if tc.wantBody != "" && strings.TrimSpace(string(data)) != tc.wantBody {
					t.Fatalf("body = %s", data)
				}
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body)), Header: make(http.Header)}, nil
			})}
			var stdout, stderr bytes.Buffer
			code := run(tc.args, func(k string) string { return tc.env[k] }, client, &stdout, &stderr)
			if code != tc.wantCode || !strings.HasPrefix(stdout.String(), tc.wantStdout) || !strings.Contains(stderr.String(), tc.wantStderr) {
				t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
			}
			if strings.Contains(stdout.String()+stderr.String(), "cli-secret-token") {
				t.Fatal("token printed")
			}
		})
	}
}
