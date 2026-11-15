package main

import (
	"bytes"
	"encoding/json"
	"errors"
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
	"unicode/utf8"
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
  search [options]       Search tasks using combinable filters (requires TODO_TOKEN)
  delete <id>            Delete a task by id (requires TODO_TOKEN)
  export [options]       Export every filtered task as CSV (requires TODO_TOKEN)
  dashboard              Show the web dashboard indicators as text (requires TODO_TOKEN)

List and search options:
  --page N               Page number, starting at 1 (default 1)
  --page-size N          Tasks per page, 1 to 100 (default 10)
  --sort FIELD           id, title, priority, status or due_date (default id)
  --order DIR            asc or desc (default asc)

Search and export filters (optional, combined with AND):
  --title TEXT           Literal title substring, case insensitive
  --priority VALUE       low, medium or high
  --status VALUE         pending, in_progress or done

Export options (the export is never paginated):
  --sort FIELD           id, title, priority, status or due_date (default id)
  --order DIR            asc or desc (default asc)
  --output FILE          Write the CSV to FILE instead of the standard output

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
	if args[0] == "list" || args[0] == "search" {
		return runList(args[0], args[1:], base, getenv("TODO_TOKEN"), client, stdout, stderr)
	}
	if args[0] == "delete" {
		return runDelete(args[1:], base, getenv("TODO_TOKEN"), client, stdout, stderr)
	}
	if args[0] == "export" {
		return runExport(args[1:], base, getenv("TODO_TOKEN"), client, stdout, stderr)
	}
	if args[0] == "dashboard" {
		return runDashboard(args[1:], base, getenv("TODO_TOKEN"), client, stdout, stderr)
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

// runList prints one page from the CLI listing or search endpoint as a text table.
func runList(command string, args []string, base, token string, client *http.Client, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	page := flags.Int("page", 1, "")
	pageSize := flags.Int("page-size", 10, "")
	sortBy := flags.String("sort", "id", "")
	order := flags.String("order", "asc", "")
	var title, priority, status string
	if command == "search" {
		flags.StringVar(&title, "title", "", "")
		flags.StringVar(&priority, "priority", "", "")
		flags.StringVar(&status, "status", "", "")
	}
	if flags.Parse(args) != nil || flags.NArg() != 0 {
		_, _ = io.WriteString(stderr, usage)
		return 2
	}
	if token == "" {
		_, _ = io.WriteString(stderr, "Set TODO_TOKEN.\n")
		return 2
	}
	query := url.Values{"page": {strconv.Itoa(*page)}, "page_size": {strconv.Itoa(*pageSize)}, "sort": {*sortBy}, "order": {*order}}
	path := "/api/cli/records"
	if command == "search" {
		path += "/search"
		query.Set("title", title)
		query.Set("priority", priority)
		query.Set("status", status)
	}
	response, code := sendAuthorized(client, http.MethodGet, base+path+"?"+query.Encode(), token, stderr)
	if code != 0 {
		return code
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusBadRequest {
		_, _ = fmt.Fprintf(stderr, "Invalid %s options: check filters, --page, --page-size, --sort and --order.\n", command)
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

// runExport downloads every task matching the filters from the CLI export
// endpoint and writes the CSV to --output, or to the standard output when the
// option is omitted. A file is only created after the API accepts the request,
// and a partially written file is removed.
func runExport(args []string, base, token string, client *http.Client, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("export", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	sortBy := flags.String("sort", "id", "")
	order := flags.String("order", "asc", "")
	title := flags.String("title", "", "")
	priority := flags.String("priority", "", "")
	status := flags.String("status", "", "")
	output := flags.String("output", "", "")
	if flags.Parse(args) != nil || flags.NArg() != 0 {
		_, _ = io.WriteString(stderr, usage)
		return 2
	}
	if token == "" {
		_, _ = io.WriteString(stderr, "Set TODO_TOKEN.\n")
		return 2
	}
	query := url.Values{"title": {*title}, "priority": {*priority}, "status": {*status}, "sort": {*sortBy}, "order": {*order}}
	response, code := sendAuthorized(client, http.MethodGet, base+"/api/cli/records/export?"+query.Encode(), token, stderr)
	if code != 0 {
		return code
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusBadRequest {
		_, _ = io.WriteString(stderr, "Invalid export options: check filters, --sort and --order.\n")
		return 2
	}
	if response.StatusCode != http.StatusOK {
		_, _ = fmt.Fprintf(stderr, "API returned HTTP %d.\n", response.StatusCode)
		return 1
	}
	if *output == "" {
		if _, err := io.Copy(stdout, response.Body); err != nil {
			_, _ = io.WriteString(stderr, "Could not write the CSV.\n")
			return 1
		}
		return 0
	}
	file, err := os.Create(*output)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "Could not create %s.\n", *output)
		return 1
	}
	_, err = io.Copy(file, response.Body)
	if err = errors.Join(err, file.Close()); err != nil {
		_ = os.Remove(*output)
		_, _ = io.WriteString(stderr, "Could not write the CSV.\n")
		return 1
	}
	_, _ = fmt.Fprintf(stdout, "CSV saved to %s.\n", *output)
	return 0
}

// indicator is one line of a dashboard section: a field value and its task count.
type indicator struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

// runDashboard prints, as text, the same three indicators shown by the web
// dashboard: tasks by status, by priority and by category, with each count and
// its percentage of the total.
func runDashboard(args []string, base, token string, client *http.Client, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		_, _ = io.WriteString(stderr, usage)
		return 2
	}
	if token == "" {
		_, _ = io.WriteString(stderr, "Set TODO_TOKEN.\n")
		return 2
	}
	response, code := sendAuthorized(client, http.MethodGet, base+"/api/cli/dashboard", token, stderr)
	if code != 0 {
		return code
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = fmt.Fprintf(stderr, "API returned HTTP %d.\n", response.StatusCode)
		return 1
	}
	var result struct {
		Total      int         `json:"total"`
		ByStatus   []indicator `json:"by_status"`
		ByPriority []indicator `json:"by_priority"`
		ByCategory []indicator `json:"by_category"`
	}
	if json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result) != nil {
		_, _ = io.WriteString(stderr, "Invalid API response.\n")
		return 1
	}
	sections := []struct {
		title string
		items []indicator
	}{{"Tasks by status", result.ByStatus}, {"Tasks by priority", result.ByPriority}, {"Tasks by category", result.ByCategory}}
	width := 0
	for _, section := range sections {
		for _, item := range section.items {
			width = max(width, utf8.RuneCountInString(item.Key))
		}
	}
	_, _ = fmt.Fprintf(stdout, "Total: %d tasks\n", result.Total)
	for _, section := range sections {
		_, _ = fmt.Fprintf(stdout, "\n%s\n", section.title)
		if len(section.items) == 0 {
			_, _ = io.WriteString(stdout, "  (none)\n")
		}
		for _, item := range section.items {
			// Rounded half up, like Math.round in the web dashboard.
			percent := 0
			if result.Total > 0 {
				percent = (item.Count*200 + result.Total) / (2 * result.Total)
			}
			padding := strings.Repeat(" ", width-utf8.RuneCountInString(item.Key))
			_, _ = fmt.Fprintf(stdout, "  %s%s  %5d  %4s\n", item.Key, padding, item.Count, strconv.Itoa(percent)+"%")
		}
	}
	return 0
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
