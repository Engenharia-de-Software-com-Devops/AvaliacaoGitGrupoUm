package main

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
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

func TestListAndDeleteCommands(t *testing.T) {
	token := map[string]string{"TODO_TOKEN": "cli-secret-token"}
	page := `{"items":[{"id":7,"title":"Write report","description":"","priority":"high","status":"pending","due_date":"2026-11-20","category":"work"},` +
		`{"id":9,"title":"Comprar café","description":"","priority":"low","status":"done","due_date":"2026-11-16","category":"casa"}],"page":2,"page_size":2,"total":5,"total_pages":3}`
	for _, tc := range []struct {
		name                 string
		args                 []string
		env                  map[string]string
		status               int
		body                 string
		failure              bool
		wantMethod, wantPath string
		wantQuery            string
		wantCode             int
		wantStdout           string
		wantStderr           string
	}{
		{name: "list defaults", args: []string{"list"}, env: token, status: 200, body: page, wantMethod: http.MethodGet, wantPath: "/api/cli/records",
			wantQuery:  "order=asc&page=1&page_size=10&sort=id",
			wantStdout: "ID  TITLE         PRIORITY  STATUS   DUE DATE    CATEGORY\n7   Write report  high      pending  2026-11-20  work\n9   Comprar café  low       done     2026-11-16  casa\nPage 2 of 3 (5 tasks)\n"},
		{name: "search defaults", args: []string{"search"}, env: token, status: 200, body: page, wantMethod: http.MethodGet, wantPath: "/api/cli/records/search", wantQuery: "order=asc&page=1&page_size=10&priority=&sort=id&status=&title=", wantStdout: "ID  TITLE"},
		{name: "search combined", args: []string{"search", "--title", "Git %_", "--priority", "high", "--status", "pending", "--page", "2", "--page-size", "5", "--sort", "title", "--order", "desc"}, env: token, status: 200, body: page, wantMethod: http.MethodGet, wantPath: "/api/cli/records/search", wantQuery: "order=desc&page=2&page_size=5&priority=high&sort=title&status=pending&title=Git+%25_", wantStdout: "ID  TITLE"},
		{name: "search empty", args: []string{"search"}, env: token, status: 200, body: `{"items":[],"page":1,"total":0,"total_pages":0}`, wantMethod: http.MethodGet, wantPath: "/api/cli/records/search", wantStdout: "No tasks found.\nPage 1 of 1 (0 tasks)\n"},
		{name: "search invalid options", args: []string{"search", "--priority", "urgent"}, env: token, status: 400, wantMethod: http.MethodGet, wantPath: "/api/cli/records/search", wantCode: 2, wantStderr: "Invalid search options"},
		{name: "search unauthorized", args: []string{"search"}, env: token, status: 401, wantMethod: http.MethodGet, wantPath: "/api/cli/records/search", wantCode: 1, wantStderr: "API returned HTTP 401."},
		{name: "search invalid response", args: []string{"search"}, env: token, status: 200, body: `{`, wantMethod: http.MethodGet, wantPath: "/api/cli/records/search", wantCode: 1, wantStderr: "Invalid API response."},
		{name: "search unknown flag", args: []string{"search", "--owner", "bob"}, env: token, wantCode: 2, wantStderr: "Usage:"},
		{name: "search no token", args: []string{"search"}, wantCode: 2, wantStderr: "Set TODO_TOKEN."},
		{name: "search network failure", args: []string{"search"}, env: token, failure: true, wantCode: 1, wantStderr: "API connection failed."},
		{name: "list options", args: []string{"list", "--page", "2", "--page-size", "2", "--sort", "title", "--order", "desc"}, env: token, status: 200, body: page,
			wantMethod: http.MethodGet, wantPath: "/api/cli/records", wantQuery: "order=desc&page=2&page_size=2&sort=title", wantStdout: "ID  TITLE"},
		{name: "list empty", args: []string{"list"}, env: token, status: 200, body: `{"items":[],"page":1,"page_size":10,"total":0,"total_pages":0}`,
			wantMethod: http.MethodGet, wantPath: "/api/cli/records", wantStdout: "No tasks found.\nPage 1 of 1 (0 tasks)\n"},
		{name: "list invalid options", args: []string{"list", "--sort", "category"}, env: token, status: 400, wantMethod: http.MethodGet, wantPath: "/api/cli/records", wantCode: 2, wantStderr: "Invalid list options"},
		{name: "list unauthorized", args: []string{"list"}, env: token, status: 401, wantMethod: http.MethodGet, wantPath: "/api/cli/records", wantCode: 1, wantStderr: "API returned HTTP 401."},
		{name: "list invalid response", args: []string{"list"}, env: token, status: 200, body: `{`, wantMethod: http.MethodGet, wantPath: "/api/cli/records", wantCode: 1, wantStderr: "Invalid API response."},
		{name: "list unknown flag", args: []string{"list", "--owner", "bob"}, env: token, wantCode: 2, wantStderr: "Usage:"},
		{name: "list non numeric page", args: []string{"list", "--page", "two"}, env: token, wantCode: 2, wantStderr: "Usage:"},
		{name: "list extra argument", args: []string{"list", "extra"}, env: token, wantCode: 2, wantStderr: "Usage:"},
		{name: "list without token", args: []string{"list"}, wantCode: 2, wantStderr: "Set TODO_TOKEN."},
		{name: "list network failure", args: []string{"list"}, env: token, failure: true, wantCode: 1, wantStderr: "API connection failed."},
		{name: "list bad URL", args: []string{"list"}, env: map[string]string{"TODO_TOKEN": "cli-secret-token", "TODO_API_URL": "://bad"}, wantCode: 1, wantStderr: "Invalid API URL."},
		{name: "delete", args: []string{"delete", "7"}, env: token, status: 204, wantMethod: http.MethodDelete, wantPath: "/api/cli/records/7", wantStdout: "Task 7 deleted.\n"},
		{name: "delete missing task", args: []string{"delete", "7"}, env: token, status: 404, wantMethod: http.MethodDelete, wantPath: "/api/cli/records/7", wantCode: 1, wantStderr: "Task 7 not found."},
		{name: "delete unauthorized", args: []string{"delete", "7"}, env: token, status: 401, wantMethod: http.MethodDelete, wantPath: "/api/cli/records/7", wantCode: 1, wantStderr: "API returned HTTP 401."},
		{name: "delete without id", args: []string{"delete"}, env: token, wantCode: 2, wantStderr: "Usage:"},
		{name: "delete with two ids", args: []string{"delete", "7", "8"}, env: token, wantCode: 2, wantStderr: "Usage:"},
		{name: "delete with text id", args: []string{"delete", "abc"}, env: token, wantCode: 2, wantStderr: "positive integer"},
		{name: "delete with zero id", args: []string{"delete", "0"}, env: token, wantCode: 2, wantStderr: "positive integer"},
		{name: "delete without token", args: []string{"delete", "7"}, wantCode: 2, wantStderr: "Set TODO_TOKEN."},
		{name: "delete network failure", args: []string{"delete", "7"}, env: token, failure: true, wantCode: 1, wantStderr: "API connection failed."},
		{name: "delete bad URL", args: []string{"delete", "7"}, env: map[string]string{"TODO_TOKEN": "cli-secret-token", "TODO_API_URL": "://bad"}, wantCode: 1, wantStderr: "Invalid API URL."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
				if tc.failure {
					return nil, errors.New("network failure")
				}
				if r.Method != tc.wantMethod || r.URL.Host != "app:8080" || r.URL.Path != tc.wantPath {
					t.Fatalf("request = %s %s", r.Method, r.URL)
				}
				if tc.wantQuery != "" && r.URL.RawQuery != tc.wantQuery {
					t.Fatalf("query = %s", r.URL.RawQuery)
				}
				if r.Header.Get("Authorization") != "Bearer cli-secret-token" {
					t.Fatalf("headers = %v", r.Header)
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

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("connection reset") }

func TestExportCommand(t *testing.T) {
	csvBody := "id,title,description,priority,status,due_date,category\n7,Write report,,high,pending,2026-11-20,work\n"
	dir := t.TempDir()
	token := map[string]string{"TODO_TOKEN": "cli-secret-token"}
	filters := []string{"export", "--title", "report", "--priority", "high", "--status", "pending", "--sort", "due_date", "--order", "desc"}
	for _, tc := range []struct {
		name       string
		args       []string
		env        map[string]string
		status     int
		body       io.Reader
		failure    bool
		wantQuery  string
		wantCode   int
		wantStdout string
		wantStderr string
		wantFile   string
	}{
		{name: "to standard output", args: filters, env: token, status: 200, body: strings.NewReader(csvBody),
			wantQuery: "order=desc&priority=high&sort=due_date&status=pending&title=report", wantStdout: csvBody},
		{name: "defaults without filters", args: []string{"export"}, env: token, status: 200, body: strings.NewReader(csvBody),
			wantQuery: "order=asc&priority=&sort=id&status=&title=", wantStdout: csvBody},
		{name: "to a file", args: []string{"export", "--output", filepath.Join(dir, "tarefas.csv")}, env: token, status: 200, body: strings.NewReader(csvBody),
			wantStdout: "CSV saved to " + filepath.Join(dir, "tarefas.csv") + ".\n", wantFile: filepath.Join(dir, "tarefas.csv")},
		{name: "file cannot be created", args: []string{"export", "--output", filepath.Join(dir, "missing", "tarefas.csv")}, env: token, status: 200, body: strings.NewReader(csvBody),
			wantCode: 1, wantStderr: "Could not create"},
		{name: "download fails while writing a file", args: []string{"export", "--output", filepath.Join(dir, "partial.csv")}, env: token, status: 200, body: failingReader{},
			wantCode: 1, wantStderr: "Could not write the CSV."},
		{name: "download fails while writing standard output", args: []string{"export"}, env: token, status: 200, body: failingReader{},
			wantCode: 1, wantStderr: "Could not write the CSV."},
		{name: "missing token", args: []string{"export"}, wantCode: 2, wantStderr: "Set TODO_TOKEN."},
		{name: "unknown flag", args: []string{"export", "--page", "2"}, env: token, wantCode: 2, wantStderr: "Usage:"},
		{name: "extra argument", args: []string{"export", "extra"}, env: token, wantCode: 2, wantStderr: "Usage:"},
		{name: "invalid options", args: []string{"export", "--priority", "urgent"}, env: token, status: 400, body: strings.NewReader("Invalid export parameters"), wantCode: 2, wantStderr: "Invalid export options"},
		{name: "unauthorized", args: []string{"export"}, env: token, status: 401, body: strings.NewReader(""), wantCode: 1, wantStderr: "API returned HTTP 401."},
		{name: "network failure", args: []string{"export"}, env: token, failure: true, wantCode: 1, wantStderr: "API connection failed."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
				if tc.failure {
					return nil, errors.New("network failure")
				}
				if r.Method != http.MethodGet || r.URL.Host != "app:8080" || r.URL.Path != "/api/cli/records/export" || r.Header.Get("Authorization") != "Bearer cli-secret-token" {
					t.Fatalf("request = %s %s %v", r.Method, r.URL, r.Header)
				}
				if tc.wantQuery != "" && r.URL.RawQuery != tc.wantQuery {
					t.Fatalf("query = %s, want %s", r.URL.RawQuery, tc.wantQuery)
				}
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(tc.body), Header: make(http.Header)}, nil
			})}
			var stdout, stderr bytes.Buffer
			code := run(tc.args, func(k string) string { return tc.env[k] }, client, &stdout, &stderr)
			if code != tc.wantCode || stdout.String() != tc.wantStdout || !strings.Contains(stderr.String(), tc.wantStderr) {
				t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
			}
			if tc.wantFile != "" {
				content, err := os.ReadFile(tc.wantFile)
				if err != nil || string(content) != csvBody {
					t.Fatalf("file content = %q, %v", content, err)
				}
			}
			if strings.Contains(stdout.String()+stderr.String(), "cli-secret-token") {
				t.Fatal("token printed")
			}
		})
	}
	if _, err := os.Stat(filepath.Join(dir, "partial.csv")); !os.IsNotExist(err) {
		t.Fatalf("partial export file was kept: %v", err)
	}
}

func TestDashboardCommand(t *testing.T) {
	token := map[string]string{"TODO_TOKEN": "cli-secret-token"}
	indicators := `{"total":3,"by_status":[{"key":"pending","count":2},{"key":"in_progress","count":0},{"key":"done","count":1}],` +
		`"by_priority":[{"key":"low","count":1},{"key":"medium","count":0},{"key":"high","count":2}],"by_category":[{"key":"trabalho","count":3}]}`
	empty := `{"total":0,"by_status":[{"key":"pending","count":0},{"key":"in_progress","count":0},{"key":"done","count":0}],` +
		`"by_priority":[{"key":"low","count":0},{"key":"medium","count":0},{"key":"high","count":0}],"by_category":[]}`
	for _, tc := range []struct {
		name       string
		args       []string
		env        map[string]string
		status     int
		body       string
		failure    bool
		wantCode   int
		wantLines  []string
		wantStderr string
	}{
		{name: "indicators", args: []string{"dashboard"}, env: token, status: 200, body: indicators, wantLines: []string{
			"Total: 3 tasks", "", "Tasks by status", "pending 2 67%", "in_progress 0 0%", "done 1 33%", "",
			"Tasks by priority", "low 1 33%", "medium 0 0%", "high 2 67%", "", "Tasks by category", "trabalho 3 100%"}},
		{name: "empty database", args: []string{"dashboard"}, env: token, status: 200, body: empty, wantLines: []string{
			"Total: 0 tasks", "", "Tasks by status", "pending 0 0%", "in_progress 0 0%", "done 0 0%", "",
			"Tasks by priority", "low 0 0%", "medium 0 0%", "high 0 0%", "", "Tasks by category", "(none)"}},
		{name: "unauthorized", args: []string{"dashboard"}, env: token, status: 401, wantCode: 1, wantStderr: "API returned HTTP 401."},
		{name: "invalid response", args: []string{"dashboard"}, env: token, status: 200, body: `{`, wantCode: 1, wantStderr: "Invalid API response."},
		{name: "extra argument", args: []string{"dashboard", "extra"}, env: token, wantCode: 2, wantStderr: "Usage:"},
		{name: "without token", args: []string{"dashboard"}, wantCode: 2, wantStderr: "Set TODO_TOKEN."},
		{name: "network failure", args: []string{"dashboard"}, env: token, failure: true, wantCode: 1, wantStderr: "API connection failed."},
		{name: "bad URL", args: []string{"dashboard"}, env: map[string]string{"TODO_TOKEN": "cli-secret-token", "TODO_API_URL": "://bad"}, wantCode: 1, wantStderr: "Invalid API URL."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
				if tc.failure {
					return nil, errors.New("network failure")
				}
				if r.Method != http.MethodGet || r.URL.Host != "app:8080" || r.URL.Path != "/api/cli/dashboard" || r.Header.Get("Authorization") != "Bearer cli-secret-token" {
					t.Fatalf("request = %s %s %v", r.Method, r.URL, r.Header)
				}
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body)), Header: make(http.Header)}, nil
			})}
			var stdout, stderr bytes.Buffer
			code := run(tc.args, func(k string) string { return tc.env[k] }, client, &stdout, &stderr)
			if code != tc.wantCode || !strings.Contains(stderr.String(), tc.wantStderr) {
				t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
			}
			if tc.wantLines != nil {
				lines := strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n")
				if len(lines) != len(tc.wantLines) {
					t.Fatalf("stdout = %q", stdout.String())
				}
				for i, line := range lines {
					if strings.Join(strings.Fields(line), " ") != tc.wantLines[i] {
						t.Fatalf("line %d = %q, want %q", i, line, tc.wantLines[i])
					}
				}
				// Counts and percentages are right-aligned in fixed columns.
				if tc.name == "indicators" && lines[3] != "  pending          2   67%" {
					t.Fatalf("unaligned output: %q", stdout.String())
				}
			}
			if strings.Contains(stdout.String()+stderr.String(), "cli-secret-token") {
				t.Fatal("token printed")
			}
		})
	}
}
