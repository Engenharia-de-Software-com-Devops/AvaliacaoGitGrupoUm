package main

import (
	"bytes"
	"context"
	"database/sql/driver"
	"errors"
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
