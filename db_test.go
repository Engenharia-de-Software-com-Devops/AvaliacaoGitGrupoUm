package main

import (
	"bytes"
	"context"
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
