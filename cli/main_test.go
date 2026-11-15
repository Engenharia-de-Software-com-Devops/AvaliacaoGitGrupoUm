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
