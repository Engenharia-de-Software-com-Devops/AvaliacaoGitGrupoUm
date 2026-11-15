package main

import (
	"bytes"
	"context"
	"database/sql/driver"
	"errors"
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
