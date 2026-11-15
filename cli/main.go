package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const usage = "Usage: cli [help|health|token|session]\n"

// run sends requests only to the CLI API; credentials are read from the environment.
func run(args []string, getenv func(string) string, client *http.Client, stdout, stderr io.Writer) int {
	if len(args) == 0 || (len(args) == 1 && (args[0] == "help" || args[0] == "-h" || args[0] == "--help")) {
		_, _ = io.WriteString(stdout, usage)
		return 0
	}
	if len(args) != 1 || (args[0] != "health" && args[0] != "token" && args[0] != "session") {
		_, _ = io.WriteString(stderr, usage)
		return 2
	}
	base := strings.TrimRight(getenv("TODO_API_URL"), "/")
	if base == "" {
		base = "http://app:8080"
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

var invoke = func() int {
	return run(os.Args[1:], os.Getenv, &http.Client{Timeout: 5 * time.Second}, os.Stdout, os.Stderr)
}
var exit = os.Exit

func main() {
	if code := invoke(); code != 0 {
		exit(code)
	}
}
