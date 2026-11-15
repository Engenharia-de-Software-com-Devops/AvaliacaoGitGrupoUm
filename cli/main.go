package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

const usage = `Usage: cli <command> [options]

Commands:
  help                   Show this help
  health                 Check GET /api/cli/health
  token                  Print a token for TODO_USERNAME and TODO_PASSWORD
  session                Print the user authenticated by TODO_TOKEN
  create [options]       Create a task (requires TODO_TOKEN)
  update <id> [options]  Replace every field of a task (requires TODO_TOKEN)
  list [options]         List tasks one page at a time (requires TODO_TOKEN)
  delete <id>            Delete a task by id (requires TODO_TOKEN)

List options:
  --page N               Page number, starting at 1 (default 1)
  --page-size N          Tasks per page, 1 to 100 (default 10)
  --sort FIELD           id, title, priority, status or due_date (default id)
  --order DIR            asc or desc (default asc)

Task options, validated by the API with the same rules as the web form:
  --title TEXT           3 to 100 characters (required)
  --description TEXT     up to 500 characters (optional)
  --priority VALUE       low, medium or high (required)
  --status VALUE         pending, in_progress or done (required; create defaults to pending)
  --due-date DATE        YYYY-MM-DD (required)
  --category TEXT        2 to 50 characters (required)
`

// task mirrors the JSON task accepted and returned by the CLI endpoints.
type task struct {
	ID          int64  `json:"id,omitempty"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Priority    string `json:"priority"`
	Status      string `json:"status"`
	DueDate     string `json:"due_date"`
	Category    string `json:"category"`
}

// run sends requests only to the CLI API; credentials are read from the environment.
func run(args []string, getenv func(string) string, client *http.Client, stdout, stderr io.Writer) int {
	if len(args) == 0 || (len(args) == 1 && (args[0] == "help" || args[0] == "-h" || args[0] == "--help")) {
		_, _ = io.WriteString(stdout, usage)
		return 0
	}
	base := strings.TrimRight(getenv("TODO_API_URL"), "/")
	if base == "" {
		base = "http://app:8080"
	}
	if args[0] == "create" || args[0] == "update" {
		return runTask(args, base, getenv("TODO_TOKEN"), client, stdout, stderr)
	}
	if args[0] == "list" {
		return runList(args[1:], base, getenv("TODO_TOKEN"), client, stdout, stderr)
	}
	if args[0] == "delete" {
		return runDelete(args[1:], base, getenv("TODO_TOKEN"), client, stdout, stderr)
	}
	if len(args) != 1 || (args[0] != "health" && args[0] != "token" && args[0] != "session") {
		_, _ = io.WriteString(stderr, usage)
		return 2
	}
	method, path := http.MethodGet, "/api/cli/"+args[0]
	var payload io.Reader
	if args[0] == "token" {
		username, password := getenv("TODO_USERNAME"), getenv("TODO_PASSWORD")
		if username == "" || password == "" {
			_, _ = io.WriteString(stderr, "Set TODO_USERNAME and TODO_PASSWORD.\n")
			return 2
		}
		data, _ := json.Marshal(map[string]string{"username": username, "password": password})
		payload, method = bytes.NewReader(data), http.MethodPost
	}
	if args[0] == "session" && getenv("TODO_TOKEN") == "" {
		_, _ = io.WriteString(stderr, "Set TODO_TOKEN.\n")
		return 2
	}
	request, err := http.NewRequest(method, base+path, payload)
	if err != nil {
		_, _ = io.WriteString(stderr, "Invalid API URL.\n")
		return 1
	}
	if args[0] == "token" {
		request.Header.Set("Content-Type", "application/json")
	}
	if args[0] == "session" {
		request.Header.Set("Authorization", "Bearer "+getenv("TODO_TOKEN"))
	}
	response, err := client.Do(request)
	if err != nil {
		_, _ = io.WriteString(stderr, "API connection failed.\n")
		return 1
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = fmt.Fprintf(stderr, "API returned HTTP %d.\n", response.StatusCode)
		return 1
	}
	if args[0] == "health" {
		_, _ = io.WriteString(stdout, "OK\n")
		return 0
	}
	var result struct {
		Token    string `json:"token"`
		Username string `json:"username"`
	}
	if json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&result) != nil {
		_, _ = io.WriteString(stderr, "Invalid API response.\n")
		return 1
	}
	value := result.Username
	if args[0] == "token" {
		value = result.Token
	}
	if value == "" {
		_, _ = io.WriteString(stderr, "Invalid API response.\n")
		return 1
	}
	_, _ = fmt.Fprintln(stdout, value)
	return 0
}

// runTask parses the create/update options, sends the task to the CLI
// endpoint with the Bearer token and prints the saved task or the per-field
// validation errors returned by the API.
func runTask(args []string, base, token string, client *http.Client, stdout, stderr io.Writer) int {
	command, rest := args[0], args[1:]
	method, path, action := http.MethodPost, "/api/cli/records", "created"
	var id int64
	if command == "update" {
		if len(rest) == 0 {
			_, _ = io.WriteString(stderr, usage)
			return 2
		}
		parsed, err := strconv.ParseInt(rest[0], 10, 64)
		if err != nil || parsed < 1 {
			_, _ = io.WriteString(stderr, "Task id must be a positive integer.\n")
			return 2
		}
		id, rest = parsed, rest[1:]
		method, path, action = http.MethodPut, "/api/cli/records/"+strconv.FormatInt(id, 10), "updated"
	}
	var input task
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&input.Title, "title", "", "")
	flags.StringVar(&input.Description, "description", "", "")
	flags.StringVar(&input.Priority, "priority", "", "")
	flags.StringVar(&input.Status, "status", map[string]string{"create": "pending"}[command], "")
	flags.StringVar(&input.DueDate, "due-date", "", "")
	flags.StringVar(&input.Category, "category", "", "")
	if flags.Parse(rest) != nil || flags.NArg() != 0 {
		_, _ = io.WriteString(stderr, usage)
		return 2
	}
	if token == "" {
		_, _ = io.WriteString(stderr, "Set TODO_TOKEN.\n")
		return 2
	}
	data, _ := json.Marshal(input)
	request, err := http.NewRequest(method, base+path, bytes.NewReader(data))
	if err != nil {
		_, _ = io.WriteString(stderr, "Invalid API URL.\n")
		return 1
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := client.Do(request)
	if err != nil {
		_, _ = io.WriteString(stderr, "API connection failed.\n")
		return 1
	}
	defer response.Body.Close()
	body := io.LimitReader(response.Body, 65536)
	switch response.StatusCode {
	case http.StatusOK, http.StatusCreated:
		var saved task
		if json.NewDecoder(body).Decode(&saved) != nil {
			_, _ = io.WriteString(stderr, "Invalid API response.\n")
			return 1
		}
		_, _ = fmt.Fprintf(stdout, "Task %d %s.\nid: %d\ntitle: %s\ndescription: %s\npriority: %s\nstatus: %s\ndue_date: %s\ncategory: %s\n",
			saved.ID, action, saved.ID, saved.Title, saved.Description, saved.Priority, saved.Status, saved.DueDate, saved.Category)
		return 0
	case http.StatusUnprocessableEntity:
		var result struct {
			Errors map[string]string `json:"errors"`
		}
		if json.NewDecoder(body).Decode(&result) != nil || len(result.Errors) == 0 {
			_, _ = io.WriteString(stderr, "Invalid API response.\n")
			return 1
		}
		fields := make([]string, 0, len(result.Errors))
		for field := range result.Errors {
			fields = append(fields, field)
		}
		sort.Strings(fields)
		_, _ = io.WriteString(stderr, "Validation failed:\n")
		for _, field := range fields {
			_, _ = fmt.Fprintf(stderr, "  %s: %s\n", field, result.Errors[field])
		}
		return 1
	case http.StatusNotFound:
		_, _ = fmt.Fprintf(stderr, "Task %d not found.\n", id)
		return 1
	}
	_, _ = fmt.Fprintf(stderr, "API returned HTTP %d.\n", response.StatusCode)
	return 1
}

// sendAuthorized sends a body-less request with the Bearer token. When the
// request cannot be built or sent it prints the reason and returns exit code 1.
func sendAuthorized(client *http.Client, method, address, token string, stderr io.Writer) (*http.Response, int) {
	request, err := http.NewRequest(method, address, nil)
	if err != nil {
		_, _ = io.WriteString(stderr, "Invalid API URL.\n")
		return nil, 1
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := client.Do(request)
	if err != nil {
		_, _ = io.WriteString(stderr, "API connection failed.\n")
		return nil, 1
	}
	return response, 0
}

// runList prints one page of tasks from GET /api/cli/records as a text table.
func runList(args []string, base, token string, client *http.Client, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("list", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	page := flags.Int("page", 1, "")
	pageSize := flags.Int("page-size", 10, "")
	sortBy := flags.String("sort", "id", "")
	order := flags.String("order", "asc", "")
	if flags.Parse(args) != nil || flags.NArg() != 0 {
		_, _ = io.WriteString(stderr, usage)
		return 2
	}
	if token == "" {
		_, _ = io.WriteString(stderr, "Set TODO_TOKEN.\n")
		return 2
	}
	query := url.Values{"page": {strconv.Itoa(*page)}, "page_size": {strconv.Itoa(*pageSize)}, "sort": {*sortBy}, "order": {*order}}
	response, code := sendAuthorized(client, http.MethodGet, base+"/api/cli/records?"+query.Encode(), token, stderr)
	if code != 0 {
		return code
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusBadRequest {
		_, _ = io.WriteString(stderr, "Invalid list options: --page from 1, --page-size from 1 to 100, --sort id, title, priority, status or due_date, --order asc or desc.\n")
		return 2
	}
	if response.StatusCode != http.StatusOK {
		_, _ = fmt.Fprintf(stderr, "API returned HTTP %d.\n", response.StatusCode)
		return 1
	}
	var result struct {
		Items      []task `json:"items"`
		Page       int    `json:"page"`
		Total      int    `json:"total"`
		TotalPages int    `json:"total_pages"`
	}
	if json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result) != nil {
		_, _ = io.WriteString(stderr, "Invalid API response.\n")
		return 1
	}
	if len(result.Items) == 0 {
		_, _ = io.WriteString(stdout, "No tasks found.\n")
	} else {
		table := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(table, "ID\tTITLE\tPRIORITY\tSTATUS\tDUE DATE\tCATEGORY")
		for _, item := range result.Items {
			_, _ = fmt.Fprintf(table, "%d\t%s\t%s\t%s\t%s\t%s\n", item.ID, item.Title, item.Priority, item.Status, item.DueDate, item.Category)
		}
		_ = table.Flush()
	}
	_, _ = fmt.Fprintf(stdout, "Page %d of %d (%d tasks)\n", result.Page, max(result.TotalPages, 1), result.Total)
	return 0
}

// runDelete removes one task by id through DELETE /api/cli/records/{id}.
func runDelete(args []string, base, token string, client *http.Client, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		_, _ = io.WriteString(stderr, usage)
		return 2
	}
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || id < 1 {
		_, _ = io.WriteString(stderr, "Task id must be a positive integer.\n")
		return 2
	}
	if token == "" {
		_, _ = io.WriteString(stderr, "Set TODO_TOKEN.\n")
		return 2
	}
	response, code := sendAuthorized(client, http.MethodDelete, base+"/api/cli/records/"+strconv.FormatInt(id, 10), token, stderr)
	if code != 0 {
		return code
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusNoContent:
		_, _ = fmt.Fprintf(stdout, "Task %d deleted.\n", id)
		return 0
	case http.StatusNotFound:
		_, _ = fmt.Fprintf(stderr, "Task %d not found.\n", id)
		return 1
	}
	_, _ = fmt.Fprintf(stderr, "API returned HTTP %d.\n", response.StatusCode)
	return 1
}

var invoke = func() int {
	return run(os.Args[1:], os.Getenv, &http.Client{Timeout: 5 * time.Second}, os.Stdout, os.Stderr)
}
var exit = os.Exit

func main() {
	if code := invoke(); code != 0 {
		exit(code)
	}
}
