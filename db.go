package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type database struct {
	conn *sql.DB
	logs io.Writer
}

func logEvent(w io.Writer, layer, operation, status, requestID, message string) {
	level := "INFO"
	if status == "error" {
		level = "ERROR"
	}
	event := struct {
		Timestamp string `json:"timestamp"`
		Level     string `json:"level"`
		Layer     string `json:"layer"`
		Client    string `json:"client"`
		Operation string `json:"operation"`
		Status    string `json:"status"`
		RequestID string `json:"request_id"`
		Message   string `json:"message"`
	}{time.Now().UTC().Format(time.RFC3339Nano), level, layer, "web", operation, status, requestID, message}
	line, _ := json.Marshal(event)
	_, _ = w.Write(append(line, '\n'))
}

func (d *database) bootstrap(ctx context.Context, username, password, requestID string) (err error) {
	defer func() {
		status := "success"
		if err != nil {
			status = "error"
		}
		logEvent(d.logs, "db", "bootstrap", status, requestID, "User initialization completed")
	}()
	if username == "" || password == "" {
		return errors.New("initial credentials are required")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = d.conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS users (username TEXT PRIMARY KEY, password_hash TEXT NOT NULL)`)
	if err != nil {
		return err
	}
	_, err = d.conn.ExecContext(ctx, `INSERT INTO users (username, password_hash) VALUES ($1, $2) ON CONFLICT (username) DO NOTHING`, username, string(hash))
	if err != nil {
		return err
	}
	_, err = d.conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS tasks (
		id BIGSERIAL PRIMARY KEY,
		title TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		priority TEXT NOT NULL CHECK (priority IN ('low', 'medium', 'high')),
		status TEXT NOT NULL CHECK (status IN ('pending', 'in_progress', 'done')),
		due_date DATE NOT NULL,
		category TEXT NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`)
	return err
}

func (d *database) authenticate(ctx context.Context, username, password, requestID string) (ok bool, err error) {
	defer func() {
		status := "success"
		if !ok || err != nil {
			status = "error"
		}
		logEvent(d.logs, "db", "authenticate", status, requestID, "Authentication lookup completed")
	}()
	var hash string
	err = d.conn.QueryRowContext(ctx, `SELECT password_hash FROM users WHERE username = $1`, username).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil, nil
}
