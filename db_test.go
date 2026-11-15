package main

import (
	"bytes"
	"context"
	"database/sql/driver"
	"errors"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"golang.org/x/crypto/bcrypt"
)

func TestDatabaseBootstrap(t *testing.T) {
	for _, tc := range []struct {
		name string
		fail string
	}{
		{name: "success"},
		{name: "create fails", fail: "create"},
		{name: "insert fails", fail: "insert"},
		{name: "tasks table fails", fail: "tasks"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			create := mock.ExpectExec("CREATE TABLE")
			if tc.fail == "create" {
				create.WillReturnError(errors.New("db failure"))
			} else {
				create.WillReturnResult(sqlmock.NewResult(0, 0))
			}
			if tc.fail != "create" {
				insert := mock.ExpectExec("INSERT INTO users").WithArgs("alice", sqlmock.AnyArg())
				if tc.fail == "insert" {
					insert.WillReturnError(errors.New("db failure"))
				} else {
					insert.WillReturnResult(sqlmock.NewResult(0, 1))
				}
			}
			if tc.fail == "" || tc.fail == "tasks" {
				tasks := mock.ExpectExec("CREATE TABLE IF NOT EXISTS tasks")
				if tc.fail == "tasks" {
					tasks.WillReturnError(errors.New("db failure"))
				} else {
					tasks.WillReturnResult(sqlmock.NewResult(0, 0))
				}
			}
			var logs bytes.Buffer
			db := database{conn: conn, logs: &logs}
			err = db.bootstrap(context.Background(), "alice", "example password", "startup")
			if (err != nil) != (tc.fail != "") {
				t.Fatalf("bootstrap error = %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(logs.String(), "example password") {
				t.Fatal("password leaked in log")
			}
		})
	}
	conn, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	db := database{conn: conn, logs: &bytes.Buffer{}}
	if err := db.bootstrap(context.Background(), "", "", "startup"); err == nil {
		t.Fatal("empty credentials accepted")
	}
	if err := db.bootstrap(context.Background(), "alice", strings.Repeat("x", 73), "startup"); err == nil {
		t.Fatal("oversized password accepted")
	}
}

func TestDatabaseAuthenticate(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("correct"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, password  string
		row             string
		queryError      error
		want, wantError bool
	}{
		{name: "valid", password: "correct", row: string(hash), want: true},
		{name: "invalid password", password: "wrong", row: string(hash)},
		{name: "missing user"},
		{name: "database failure", queryError: errors.New("database unavailable"), wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			query := mock.ExpectQuery("SELECT password_hash FROM users").WithArgs("alice")
			if tc.queryError != nil {
				query.WillReturnError(tc.queryError)
			} else if tc.row == "" {
				query.WillReturnRows(sqlmock.NewRows([]string{"password_hash"}))
			} else {
				query.WillReturnRows(sqlmock.NewRows([]string{"password_hash"}).AddRow(tc.row))
			}
			var logs bytes.Buffer
			db := database{conn: conn, logs: &logs}
			got, err := db.authenticate(context.Background(), "alice", tc.password, "req-123")
			if got != tc.want || (err != nil) != tc.wantError {
				t.Fatalf("authenticate = %v, %v", got, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(logs.String(), `"request_id":"req-123"`) || (tc.password != "" && strings.Contains(logs.String(), tc.password)) {
				t.Fatal("invalid or unsafe db log")
			}
		})
	}
}

func TestDatabaseAuthenticateCLI(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("correct"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, password, row string
		queryErr            error
		want, wantErr       bool
	}{
		{name: "valid", password: "correct", row: string(hash), want: true},
		{name: "wrong", password: "incorrect", row: string(hash)},
		{name: "missing user"},
		{name: "query fails", queryErr: errors.New("database failure"), wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			query := mock.ExpectQuery("SELECT password_hash FROM users").WithArgs("alice")
			if tc.queryErr != nil {
				query.WillReturnError(tc.queryErr)
			} else if tc.row == "" {
				query.WillReturnRows(sqlmock.NewRows([]string{"password_hash"}))
			} else {
				query.WillReturnRows(sqlmock.NewRows([]string{"password_hash"}).AddRow(tc.row))
			}
			var logs bytes.Buffer
			got, err := (&database{conn: conn, logs: &logs}).authenticateCLI(context.Background(), "alice", tc.password, "req-cli")
			if got != tc.want || (err != nil) != tc.wantErr {
				t.Fatalf("authenticate = %v, %v", got, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(logs.String(), `"client":"cli","operation":"authenticate"`) || !strings.Contains(logs.String(), `"request_id":"req-cli"`) || (tc.password != "" && strings.Contains(logs.String(), tc.password)) {
				t.Fatalf("invalid db log: %s", logs.String())
			}
		})
	}
}

var sampleTask = task{Title: "Write report", Description: "Quarterly numbers", Priority: "high", Status: "pending", DueDate: "2026-11-20", Category: "work"}

func taskRows(t task) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "title", "description", "priority", "status", "due_date", "category"}).
		AddRow(t.ID, t.Title, t.Description, t.Priority, t.Status, t.DueDate, t.Category)
}

func TestDatabaseWebTasks(t *testing.T) {
	stored := sampleTask
	stored.ID = 7
	args := []driver.Value{sampleTask.Title, sampleTask.Description, sampleTask.Priority, sampleTask.Status, sampleTask.DueDate, sampleTask.Category}
	for _, tc := range []struct {
		name, operation, query string
		args                   []driver.Value
		call                   func(*database) (task, error)
	}{
		{name: "insert", operation: "insert", query: "INSERT INTO tasks", args: args, call: func(d *database) (task, error) {
			return d.createWebTask(context.Background(), sampleTask, "req-9")
		}},
		{name: "update", operation: "update", query: "UPDATE tasks SET", args: append(append([]driver.Value{}, args...), int64(7)), call: func(d *database) (task, error) {
			return d.updateWebTask(context.Background(), 7, sampleTask, "req-9")
		}},
		{name: "select", operation: "select", query: "SELECT (.+) FROM tasks WHERE id", args: []driver.Value{int64(7)}, call: func(d *database) (task, error) {
			return d.getWebTask(context.Background(), 7, "req-9")
		}},
	} {
		for _, outcome := range []string{"success", "not found", "failure"} {
			t.Run(tc.name+" "+outcome, func(t *testing.T) {
				conn, mock, err := sqlmock.New()
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Close()
				query := mock.ExpectQuery(tc.query).WithArgs(tc.args...)
				switch outcome {
				case "success":
					query.WillReturnRows(taskRows(stored))
				case "not found":
					query.WillReturnRows(sqlmock.NewRows([]string{"id"}))
				default:
					query.WillReturnError(errors.New("database unavailable"))
				}
				var logs bytes.Buffer
				got, err := tc.call(&database{conn: conn, logs: &logs})
				switch outcome {
				case "success":
					if err != nil || got != stored {
						t.Fatalf("got %+v, %v", got, err)
					}
				case "not found":
					if !errors.Is(err, errTaskNotFound) {
						t.Fatalf("error = %v, want errTaskNotFound", err)
					}
				default:
					if err == nil || errors.Is(err, errTaskNotFound) {
						t.Fatalf("error = %v, want database failure", err)
					}
				}
				if err := mock.ExpectationsWereMet(); err != nil {
					t.Fatal(err)
				}
				want := `"layer":"db","client":"web","operation":"` + tc.operation + `","status":"`
				if outcome == "success" {
					want += `success"`
				} else {
					want += `error"`
				}
				if !strings.Contains(logs.String(), want) || !strings.Contains(logs.String(), `"request_id":"req-9"`) {
					t.Fatalf("unexpected db log: %s", logs.String())
				}
			})
		}
	}
}

func expectDBLog(t *testing.T, logs, operation, status string) {
	t.Helper()
	want := `"layer":"db","client":"web","operation":"` + operation + `","status":"` + status + `"`
	if !strings.Contains(logs, want) || !strings.Contains(logs, `"request_id":"req-list"`) {
		t.Fatalf("unexpected db log: %s", logs)
	}
}

func TestDatabaseWebTaskList(t *testing.T) {
	first, second := sampleTask, sampleTask
	first.ID, second.ID, second.Title = 6, 9, "Another task"
	for _, tc := range []struct {
		name, sort, order, orderBy, fail string
		page                             int
	}{
		{name: "default order", sort: "id", order: "asc", orderBy: "id ASC, id ASC", page: 1},
		{name: "title descending", sort: "title", order: "desc", orderBy: "lower(title) DESC, id DESC", page: 2},
		{name: "priority rank", sort: "priority", order: "asc", orderBy: "CASE priority WHEN 'low' THEN 1 WHEN 'medium' THEN 2 ELSE 3 END ASC, id ASC", page: 1},
		{name: "status rank", sort: "status", order: "desc", orderBy: "CASE status WHEN 'pending' THEN 1 WHEN 'in_progress' THEN 2 ELSE 3 END DESC, id DESC", page: 1},
		{name: "due date", sort: "due_date", order: "asc", orderBy: "due_date ASC, id ASC", page: 1},
		{name: "count fails", sort: "id", order: "asc", page: 1, fail: "count"},
		{name: "select fails", sort: "id", order: "asc", orderBy: "id ASC, id ASC", page: 1, fail: "select"},
		{name: "scan fails", sort: "id", order: "asc", orderBy: "id ASC, id ASC", page: 1, fail: "scan"},
		{name: "row error", sort: "id", order: "asc", orderBy: "id ASC, id ASC", page: 1, fail: "rows"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			count := mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM tasks"))
			if tc.fail == "count" {
				count.WillReturnError(errors.New("database unavailable"))
			} else {
				count.WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(6))
				query := mock.ExpectQuery(regexp.QuoteMeta("FROM tasks ORDER BY "+tc.orderBy+" LIMIT $1 OFFSET $2")).WithArgs(5, (tc.page-1)*5)
				switch tc.fail {
				case "select":
					query.WillReturnError(errors.New("database unavailable"))
				case "scan":
					query.WillReturnRows(sqlmock.NewRows([]string{"id", "title", "description", "priority", "status", "due_date", "category"}).
						AddRow("not a number", "x", "", "low", "pending", "2026-11-20", "work"))
				case "rows":
					query.WillReturnRows(taskRows(first).RowError(0, errors.New("connection lost")))
				default:
					query.WillReturnRows(taskRows(first).AddRow(second.ID, second.Title, second.Description, second.Priority, second.Status, second.DueDate, second.Category))
				}
			}
			var logs bytes.Buffer
			page, err := (&database{conn: conn, logs: &logs}).listWebTasks(context.Background(), taskPageQuery{Page: tc.page, Sort: tc.sort, Order: tc.order}, "req-list")
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
			if tc.fail != "" {
				if err == nil {
					t.Fatal("database failure was not reported")
				}
				expectDBLog(t, logs.String(), "select", "error")
				return
			}
			if err != nil || page.Page != tc.page || page.PageSize != 5 || page.Total != 6 || page.TotalPages != 2 {
				t.Fatalf("page = %+v, %v", page, err)
			}
			if len(page.Items) != 2 || page.Items[0] != first || page.Items[1] != second {
				t.Fatalf("items = %+v", page.Items)
			}
			expectDBLog(t, logs.String(), "select", "success")
		})
	}
}

func TestDatabaseWebTaskListEmpty(t *testing.T) {
	conn, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM tasks")).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery("ORDER BY").WithArgs(5, 0).WillReturnRows(sqlmock.NewRows([]string{"id", "title", "description", "priority", "status", "due_date", "category"}))
	page, err := (&database{conn: conn, logs: &bytes.Buffer{}}).listWebTasks(context.Background(), taskPageQuery{Page: 1, Sort: "id", Order: "asc"}, "req-list")
	if err != nil || page.Items == nil || len(page.Items) != 0 || page.TotalPages != 0 {
		t.Fatalf("empty page = %+v, %v", page, err)
	}
}

func TestDatabaseWebTaskDelete(t *testing.T) {
	for _, tc := range []struct {
		name    string
		result  driver.Result
		execErr error
		want    error
	}{
		{name: "deleted", result: sqlmock.NewResult(0, 1)},
		{name: "not found", result: sqlmock.NewResult(0, 0), want: errTaskNotFound},
		{name: "exec fails", execErr: errors.New("database unavailable")},
		{name: "rows affected fails", result: sqlmock.NewErrorResult(errors.New("driver failure"))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			exec := mock.ExpectExec(regexp.QuoteMeta("DELETE FROM tasks WHERE id = $1")).WithArgs(int64(7))
			if tc.execErr != nil {
				exec.WillReturnError(tc.execErr)
			} else {
				exec.WillReturnResult(tc.result)
			}
			var logs bytes.Buffer
			err = (&database{conn: conn, logs: &logs}).deleteWebTask(context.Background(), 7, "req-list")
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
			switch {
			case tc.name == "deleted":
				if err != nil {
					t.Fatalf("delete error = %v", err)
				}
				expectDBLog(t, logs.String(), "delete", "success")
			case tc.want != nil:
				if !errors.Is(err, tc.want) {
					t.Fatalf("error = %v, want %v", err, tc.want)
				}
				expectDBLog(t, logs.String(), "delete", "error")
			default:
				if err == nil || errors.Is(err, errTaskNotFound) {
					t.Fatalf("error = %v, want database failure", err)
				}
				expectDBLog(t, logs.String(), "delete", "error")
			}
		})
	}
}

func TestDatabaseWebTaskSearch(t *testing.T) {
	for _, tc := range []struct {
		name, order, fail string
		total             int
	}{
		{name: "combined filters", order: "asc", total: 6},
		{name: "descending", order: "desc", total: 6},
		{name: "empty", order: "asc"},
		{name: "count fails", order: "asc", fail: "count"},
		{name: "query fails", order: "asc", fail: "query"},
		{name: "scan fails", order: "asc", fail: "scan"},
		{name: "iteration fails", order: "asc", fail: "rows"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			where := `WHERE ($1 = '' OR strpos(lower(title), lower($1)) > 0) AND ($2 = '' OR priority = $2) AND ($3 = '' OR status = $3)`
			count := mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM tasks "+where)).WithArgs("%_'", "high", "pending")
			if tc.fail == "count" {
				count.WillReturnError(errors.New("count failure"))
			} else {
				count.WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(tc.total))
				direction := strings.ToUpper(tc.order)
				selectQuery := mock.ExpectQuery(regexp.QuoteMeta("FROM tasks "+where+" ORDER BY lower(title) "+direction+", id "+direction+" LIMIT $4 OFFSET $5")).WithArgs("%_'", "high", "pending", 5, 5)
				switch tc.fail {
				case "query":
					selectQuery.WillReturnError(errors.New("query failure"))
				case "scan":
					selectQuery.WillReturnRows(sqlmock.NewRows([]string{"id", "title", "description", "priority", "status", "due_date", "category"}).AddRow("bad", "x", "", "high", "pending", "2026-11-20", "work"))
				case "rows":
					selectQuery.WillReturnRows(taskRows(sampleTask).RowError(0, errors.New("iteration failure")))
				default:
					rows := sqlmock.NewRows([]string{"id", "title", "description", "priority", "status", "due_date", "category"})
					if tc.total > 0 {
						rows.AddRow(int64(7), sampleTask.Title, sampleTask.Description, sampleTask.Priority, sampleTask.Status, sampleTask.DueDate, sampleTask.Category)
					}
					selectQuery.WillReturnRows(rows)
				}
			}
			var logs bytes.Buffer
			query := taskSearchQuery{taskPageQuery: taskPageQuery{Page: 2, Sort: "title", Order: tc.order}, Title: "%_'", Priority: "high", Status: "pending"}
			page, err := (&database{conn: conn, logs: &logs}).searchWebTasks(context.Background(), query, "req-list")
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
			if tc.fail != "" {
				if err == nil {
					t.Fatal("expected search failure")
				}
				expectDBLog(t, logs.String(), "search", "error")
				return
			}
			if err != nil || page.Items == nil || page.Page != 2 || page.Total != tc.total || page.TotalPages != (tc.total+4)/5 {
				t.Fatalf("page = %+v, error %v", page, err)
			}
			expectDBLog(t, logs.String(), "search", "success")
		})
	}
}

func TestDatabaseWebTaskExport(t *testing.T) {
	where := `WHERE ($1 = '' OR strpos(lower(title), lower($1)) > 0) AND ($2 = '' OR priority = $2) AND ($3 = '' OR status = $3)`
	for _, tc := range []struct {
		name, order, fail string
		rows              int
	}{
		{name: "more than one page", order: "asc", rows: 7},
		{name: "descending", order: "desc", rows: 1},
		{name: "empty", order: "asc"},
		{name: "query fails", order: "asc", fail: "query"},
		{name: "scan fails", order: "asc", fail: "scan"},
		{name: "iteration fails", order: "asc", fail: "rows"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			direction := strings.ToUpper(tc.order)
			// The export query has no LIMIT/OFFSET: every filtered row is returned.
			expected := mock.ExpectQuery(regexp.QuoteMeta("FROM tasks "+where+" ORDER BY due_date "+direction+", id "+direction)+"$").WithArgs("report", "high", "")
			switch tc.fail {
			case "query":
				expected.WillReturnError(errors.New("query failure"))
			case "scan":
				expected.WillReturnRows(sqlmock.NewRows([]string{"id", "title", "description", "priority", "status", "due_date", "category"}).AddRow("bad", "x", "", "high", "pending", "2026-11-20", "work"))
			case "rows":
				expected.WillReturnRows(taskRows(sampleTask).RowError(0, errors.New("iteration failure")))
			default:
				rows := sqlmock.NewRows([]string{"id", "title", "description", "priority", "status", "due_date", "category"})
				for i := 1; i <= tc.rows; i++ {
					rows.AddRow(int64(i), sampleTask.Title, sampleTask.Description, sampleTask.Priority, sampleTask.Status, sampleTask.DueDate, sampleTask.Category)
				}
				expected.WillReturnRows(rows)
			}
			var logs bytes.Buffer
			query := taskExportQuery{Title: "report", Priority: "high", Sort: "due_date", Order: tc.order}
			items, err := (&database{conn: conn, logs: &logs}).exportWebTasks(context.Background(), query, "req-list")
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
			if tc.fail != "" {
				if err == nil || items != nil {
					t.Fatalf("expected export failure, got %v, %v", items, err)
				}
				expectDBLog(t, logs.String(), "export", "error")
				return
			}
			if err != nil || items == nil || len(items) != tc.rows {
				t.Fatalf("items = %+v, error %v", items, err)
			}
			expectDBLog(t, logs.String(), "export", "success")
		})
	}
}

func countRows(pairs ...any) *sqlmock.Rows {
	rows := sqlmock.NewRows([]string{"key", "count"})
	for i := 0; i < len(pairs); i += 2 {
		rows.AddRow(pairs[i], pairs[i+1])
	}
	return rows
}

func TestDatabaseWebDashboard(t *testing.T) {
	statusQuery := regexp.QuoteMeta("AS s(rank, key) LEFT JOIN tasks t ON t.status = s.key")
	priorityQuery := regexp.QuoteMeta("AS p(rank, key) LEFT JOIN tasks t ON t.priority = p.key")
	categoryQuery := regexp.QuoteMeta("SELECT category, COUNT(*) FROM tasks GROUP BY category ORDER BY COUNT(*) DESC, category")
	for _, fail := range []string{"", "status", "priority", "category", "scan", "rows"} {
		name := fail
		if name == "" {
			name = "success"
		}
		t.Run(name, func(t *testing.T) {
			conn, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			status := mock.ExpectQuery(statusQuery)
			if fail == "status" {
				status.WillReturnError(errors.New("database unavailable"))
			} else {
				status.WillReturnRows(countRows("pending", 3, "in_progress", 1, "done", 2))
				priority := mock.ExpectQuery(priorityQuery)
				if fail == "priority" {
					priority.WillReturnError(errors.New("database unavailable"))
				} else {
					priority.WillReturnRows(countRows("low", 0, "medium", 4, "high", 2))
					category := mock.ExpectQuery(categoryQuery)
					switch fail {
					case "category":
						category.WillReturnError(errors.New("database unavailable"))
					case "scan":
						category.WillReturnRows(countRows("work", "many"))
					case "rows":
						category.WillReturnRows(countRows("work", 4).RowError(0, errors.New("connection lost")))
					default:
						category.WillReturnRows(countRows("work", 4, "home", 2))
					}
				}
			}
			var logs bytes.Buffer
			got, err := (&database{conn: conn, logs: &logs}).webDashboard(context.Background(), "req-list")
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
			if fail != "" {
				if err == nil {
					t.Fatal("database failure was not reported")
				}
				expectDBLog(t, logs.String(), "dashboard", "error")
				return
			}
			want := dashboard{
				Total:      6,
				ByStatus:   []countItem{{"pending", 3}, {"in_progress", 1}, {"done", 2}},
				ByPriority: []countItem{{"low", 0}, {"medium", 4}, {"high", 2}},
				ByCategory: []countItem{{"work", 4}, {"home", 2}},
			}
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("dashboard = %+v, %v", got, err)
			}
			expectDBLog(t, logs.String(), "dashboard", "success")
		})
	}
}

func TestDatabaseWebDashboardEmpty(t *testing.T) {
	conn, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	mock.ExpectQuery("t.status").WillReturnRows(countRows("pending", 0, "in_progress", 0, "done", 0))
	mock.ExpectQuery("t.priority").WillReturnRows(countRows("low", 0, "medium", 0, "high", 0))
	mock.ExpectQuery("GROUP BY category").WillReturnRows(countRows())
	got, err := (&database{conn: conn, logs: &bytes.Buffer{}}).webDashboard(context.Background(), "req-list")
	if err != nil || got.Total != 0 || got.ByCategory == nil || len(got.ByCategory) != 0 {
		t.Fatalf("empty dashboard = %+v, %v", got, err)
	}
}
