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
	if status, _ := call(http.MethodGet, "/api/web/records", "", false); status != http.StatusUnauthorized {
		t.Fatalf("unauthenticated list status = %d", status)
	}
	if status, _ := call(http.MethodDelete, recordPath, "", false); status != http.StatusUnauthorized {
		t.Fatalf("unauthenticated delete status = %d", status)
	}
	if status, _ := call(http.MethodGet, "/api/web/records?sort=description", "", true); status != http.StatusBadRequest {
		t.Fatalf("invalid sort status = %d", status)
	}
	// Six more tasks guarantee at least two pages of five.
	ids := []int64{created.ID}
	prefix := fmt.Sprintf("SearchRun%d", time.Now().UnixNano())
	for _, title := range []string{prefix + " f", prefix + " b", prefix + " d", prefix + " a", prefix + " e", prefix + " %_literal"} {
		status, body := call(http.MethodPost, "/api/web/records", strings.Replace(newTask, "Integration task", title, 1), true)
		var extra struct {
			ID int64 `json:"id"`
		}
		if status != http.StatusCreated || json.Unmarshal(body, &extra) != nil {
			t.Fatalf("create listing task status = %d, body %s", status, body)
		}
		ids = append(ids, extra.ID)
	}
	type listPage struct {
		Items []struct {
			ID    int64  `json:"id"`
			Title string `json:"title"`
		} `json:"items"`
		Total      int `json:"total"`
		TotalPages int `json:"total_pages"`
	}
	list := func(query string) listPage {
		status, body := call(http.MethodGet, "/api/web/records"+query, "", true)
		var page listPage
		if status != http.StatusOK || json.Unmarshal(body, &page) != nil {
			t.Fatalf("list %s status = %d, body %s", query, status, body)
		}
		return page
	}
	first := list("?sort=title&order=asc")
	if len(first.Items) != 5 || first.Total < 7 || first.TotalPages < 2 {
		t.Fatalf("first page = %+v", first)
	}
	for i := 1; i < len(first.Items); i++ {
		if strings.ToLower(first.Items[i-1].Title) > strings.ToLower(first.Items[i].Title) {
			t.Fatalf("titles not ascending: %+v", first.Items)
		}
	}
	last := list(fmt.Sprintf("?sort=title&order=asc&page=%d", first.TotalPages))
	if len(last.Items) < 1 || len(last.Items) > 5 {
		t.Fatalf("last page = %+v", last)
	}
	if status, _ := call(http.MethodGet, "/api/web/records/search", "", false); status != http.StatusUnauthorized {
		t.Fatalf("unauthenticated search status = %d", status)
	}
	for _, invalid := range []string{"?priority=urgent", "?status=unknown", "?sort=description", "?page=0"} {
		if status, _ := call(http.MethodGet, "/api/web/records/search"+invalid, "", true); status != http.StatusBadRequest {
			t.Fatalf("invalid search %s status = %d", invalid, status)
		}
	}
	search := func(values url.Values) listPage {
		status, body := call(http.MethodGet, "/api/web/records/search?"+values.Encode(), "", true)
		var result listPage
		if status != http.StatusOK || json.Unmarshal(body, &result) != nil {
			t.Fatalf("search status = %d, body %s", status, body)
		}
		return result
	}
	filters := url.Values{"title": {strings.ToLower(prefix)}, "priority": {"high"}, "status": {"pending"}, "sort": {"title"}, "order": {"asc"}}
	for _, values := range []url.Values{
		{"title": {prefix}},
		{"title": {prefix}, "priority": {"high"}},
		{"title": {prefix}, "status": {"pending"}},
	} {
		if result := search(values); result.Total != 6 {
			t.Fatalf("partial filters %v = %+v", values, result)
		}
	}
	filtered := search(filters)
	if filtered.Total != 6 || filtered.TotalPages != 2 || len(filtered.Items) != 5 {
		t.Fatalf("combined search first page = %+v", filtered)
	}
	for _, item := range filtered.Items {
		if !strings.HasPrefix(item.Title, prefix) {
			t.Fatalf("unrelated search result = %+v", item)
		}
	}
	filters.Set("page", "2")
	if second := search(filters); second.Total != 6 || len(second.Items) != 1 {
		t.Fatalf("combined search second page = %+v", second)
	}
	if literal := search(url.Values{"title": {prefix + " %_"}}); literal.Total != 1 || len(literal.Items) != 1 {
		t.Fatalf("literal search = %+v", literal)
	}
	if none := search(url.Values{"title": {prefix}, "priority": {"low"}, "status": {"pending"}}); none.Total != 0 || len(none.Items) != 0 {
		t.Fatalf("nonmatching search = %+v", none)
	}
	if status, _ := call(http.MethodDelete, recordPath, "", true); status != http.StatusNoContent {
		t.Fatalf("delete status = %d", status)
	}
	if status, _ := call(http.MethodDelete, recordPath, "", true); status != http.StatusNotFound {
		t.Fatalf("repeated delete status = %d, want 404", status)
	}
	var remaining int
	if err := db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE id = $1`, created.ID).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("deleted task still persisted: %d, %v", remaining, err)
	}
	for _, id := range ids[1:] {
		if status, _ := call(http.MethodDelete, fmt.Sprintf("/api/web/records/%d", id), "", true); status != http.StatusNoContent {
			t.Fatalf("cleanup delete %d status = %d", id, status)
		}
	}
	fmt.Println("health, login, authenticated session, rejection, password hash, task create/edit persistence, paginated sorted listing and deletion verified")
}
