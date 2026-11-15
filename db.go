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

// task is a ToDo List record shared by the web and, later, the CLI endpoints.
type task struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Priority    string `json:"priority"`
	Status      string `json:"status"`
	DueDate     string `json:"due_date"`
	Category    string `json:"category"`
}

var errTaskNotFound = errors.New("task not found")

const webTaskColumns = `id, title, description, priority, status, to_char(due_date, 'YYYY-MM-DD'), category`

func (d *database) logOutcome(operation, requestID, message string, err error) {
	status := "success"
	if err != nil {
		status = "error"
	}
	logEvent(d.logs, "db", operation, status, requestID, message)
}

// scanTask reads a single task row and maps a missing row to errTaskNotFound.
func scanTask(row *sql.Row) (found task, err error) {
	err = row.Scan(&found.ID, &found.Title, &found.Description, &found.Priority, &found.Status, &found.DueDate, &found.Category)
	if errors.Is(err, sql.ErrNoRows) {
		return task{}, errTaskNotFound
	}
	return found, err
}

func (d *database) createWebTask(ctx context.Context, input task, requestID string) (created task, err error) {
	defer func() { d.logOutcome("insert", requestID, "Web task persisted", err) }()
	return scanTask(d.conn.QueryRowContext(ctx, `INSERT INTO tasks (title, description, priority, status, due_date, category)
		VALUES ($1, $2, $3, $4, $5::date, $6)
		RETURNING `+webTaskColumns,
		input.Title, input.Description, input.Priority, input.Status, input.DueDate, input.Category))
}

func (d *database) updateWebTask(ctx context.Context, id int64, input task, requestID string) (updated task, err error) {
	defer func() { d.logOutcome("update", requestID, "Web task updated", err) }()
	return scanTask(d.conn.QueryRowContext(ctx, `UPDATE tasks SET title = $1, description = $2, priority = $3, status = $4,
		due_date = $5::date, category = $6, updated_at = now()
		WHERE id = $7
		RETURNING `+webTaskColumns,
		input.Title, input.Description, input.Priority, input.Status, input.DueDate, input.Category, id))
}

func (d *database) getWebTask(ctx context.Context, id int64, requestID string) (found task, err error) {
	defer func() { d.logOutcome("select", requestID, "Web task loaded", err) }()
	return scanTask(d.conn.QueryRowContext(ctx, `SELECT `+webTaskColumns+` FROM tasks WHERE id = $1`, id))
}

// webPageSize is the fixed number of tasks shown per page in the web table.
const webPageSize = 5

// webTaskSortColumns maps each sortable web column to its ORDER BY expression.
// Priority and status sort by their logical rank instead of alphabetically.
// The API only accepts keys from this map, so no user input reaches the SQL text.
var webTaskSortColumns = map[string]string{
	"id":       "id",
	"title":    "lower(title)",
	"priority": "CASE priority WHEN 'low' THEN 1 WHEN 'medium' THEN 2 ELSE 3 END",
	"status":   "CASE status WHEN 'pending' THEN 1 WHEN 'in_progress' THEN 2 ELSE 3 END",
	"due_date": "due_date",
}

// taskPageQuery selects one page of the web listing and its ordering.
type taskPageQuery struct {
	Page  int
	Sort  string
	Order string
}

// taskSearchQuery combines web-specific search filters with paging and sorting.
type taskSearchQuery struct {
	taskPageQuery
	Title    string
	Priority string
	Status   string
}

// taskPage is one page of tasks plus the totals needed for pagination.
type taskPage struct {
	Items      []task `json:"items"`
	Page       int    `json:"page"`
	PageSize   int    `json:"page_size"`
	Total      int    `json:"total"`
	TotalPages int    `json:"total_pages"`
}

func (d *database) listWebTasks(ctx context.Context, query taskPageQuery, requestID string) (page taskPage, err error) {
	defer func() { d.logOutcome("select", requestID, "Web task page loaded", err) }()
	page = taskPage{Items: []task{}, Page: query.Page, PageSize: webPageSize}
	if err = d.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks`).Scan(&page.Total); err != nil {
		return taskPage{}, err
	}
	page.TotalPages = (page.Total + webPageSize - 1) / webPageSize
	direction := "ASC"
	if query.Order == "desc" {
		direction = "DESC"
	}
	rows, err := d.conn.QueryContext(ctx, `SELECT `+webTaskColumns+` FROM tasks ORDER BY `+webTaskSortColumns[query.Sort]+` `+direction+`, id `+direction+` LIMIT $1 OFFSET $2`,
		webPageSize, (query.Page-1)*webPageSize)
	if err != nil {
		return taskPage{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var item task
		if err = rows.Scan(&item.ID, &item.Title, &item.Description, &item.Priority, &item.Status, &item.DueDate, &item.Category); err != nil {
			return taskPage{}, err
		}
		page.Items = append(page.Items, item)
	}
	if err = rows.Err(); err != nil {
		return taskPage{}, err
	}
	return page, nil
}

// searchWebTasks uses its own parameterized web query for both count and page.
func (d *database) searchWebTasks(ctx context.Context, query taskSearchQuery, requestID string) (page taskPage, err error) {
	defer func() { d.logOutcome("search", requestID, "Web task search completed", err) }()
	page = taskPage{Items: []task{}, Page: query.Page, PageSize: webPageSize}
	const where = `WHERE ($1 = '' OR strpos(lower(title), lower($1)) > 0) AND ($2 = '' OR priority = $2) AND ($3 = '' OR status = $3)`
	if err = d.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks `+where, query.Title, query.Priority, query.Status).Scan(&page.Total); err != nil {
		return taskPage{}, err
	}
	page.TotalPages = (page.Total + webPageSize - 1) / webPageSize
	direction := "ASC"
	if query.Order == "desc" {
		direction = "DESC"
	}
	rows, err := d.conn.QueryContext(ctx, `SELECT `+webTaskColumns+` FROM tasks `+where+` ORDER BY `+webTaskSortColumns[query.Sort]+` `+direction+`, id `+direction+` LIMIT $4 OFFSET $5`,
		query.Title, query.Priority, query.Status, webPageSize, (query.Page-1)*webPageSize)
	if err != nil {
		return taskPage{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var item task
		if err = rows.Scan(&item.ID, &item.Title, &item.Description, &item.Priority, &item.Status, &item.DueDate, &item.Category); err != nil {
			return taskPage{}, err
		}
		page.Items = append(page.Items, item)
	}
	if err = rows.Err(); err != nil {
		return taskPage{}, err
	}
	return page, nil
}

func (d *database) deleteWebTask(ctx context.Context, id int64, requestID string) (err error) {
	defer func() { d.logOutcome("delete", requestID, "Web task deleted", err) }()
	result, err := d.conn.ExecContext(ctx, `DELETE FROM tasks WHERE id = $1`, id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return errTaskNotFound
	}
	return nil
}

// taskExportQuery carries the web search filters and ordering of a CSV export.
// It has no page: the export always contains every matching task.
type taskExportQuery struct {
	Title    string
	Priority string
	Status   string
	Sort     string
	Order    string
}

// exportWebTasks uses its own web query to return every task matching the
// filters, in the requested order and without LIMIT/OFFSET.
func (d *database) exportWebTasks(ctx context.Context, query taskExportQuery, requestID string) (items []task, err error) {
	defer func() { d.logOutcome("export", requestID, "Web task export loaded", err) }()
	direction := "ASC"
	if query.Order == "desc" {
		direction = "DESC"
	}
	rows, err := d.conn.QueryContext(ctx, `SELECT `+webTaskColumns+` FROM tasks
		WHERE ($1 = '' OR strpos(lower(title), lower($1)) > 0) AND ($2 = '' OR priority = $2) AND ($3 = '' OR status = $3)
		ORDER BY `+webTaskSortColumns[query.Sort]+` `+direction+`, id `+direction,
		query.Title, query.Priority, query.Status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items = []task{}
	for rows.Next() {
		var item task
		if err = rows.Scan(&item.ID, &item.Title, &item.Description, &item.Priority, &item.Status, &item.DueDate, &item.Category); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

// countItem is one bar or slice of a dashboard chart: a field value and how
// many tasks have it.
type countItem struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

// dashboard holds the three web indicators: tasks by status, by priority and
// by category, plus the total number of tasks.
type dashboard struct {
	Total      int         `json:"total"`
	ByStatus   []countItem `json:"by_status"`
	ByPriority []countItem `json:"by_priority"`
	ByCategory []countItem `json:"by_category"`
}

// Status and priority list every allowed value in its logical order, including
// the ones without tasks, so the charts always show the same bars.
const (
	webDashboardStatusQuery = `SELECT s.key, COUNT(t.id)
		FROM (VALUES (1, 'pending'), (2, 'in_progress'), (3, 'done')) AS s(rank, key) LEFT JOIN tasks t ON t.status = s.key
		GROUP BY s.rank, s.key ORDER BY s.rank`
	webDashboardPriorityQuery = `SELECT p.key, COUNT(t.id)
		FROM (VALUES (1, 'low'), (2, 'medium'), (3, 'high')) AS p(rank, key) LEFT JOIN tasks t ON t.priority = p.key
		GROUP BY p.rank, p.key ORDER BY p.rank`
	webDashboardCategoryQuery = `SELECT category, COUNT(*) FROM tasks GROUP BY category ORDER BY COUNT(*) DESC, category`
)

// countBy runs one aggregation query returning (key, count) rows.
func (d *database) countBy(ctx context.Context, query string) ([]countItem, error) {
	rows, err := d.conn.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []countItem{}
	for rows.Next() {
		var item countItem
		if err := rows.Scan(&item.Key, &item.Count); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// webDashboard aggregates the web indicators in the database with its own queries.
func (d *database) webDashboard(ctx context.Context, requestID string) (result dashboard, err error) {
	defer func() { d.logOutcome("dashboard", requestID, "Web dashboard indicators loaded", err) }()
	if result.ByStatus, err = d.countBy(ctx, webDashboardStatusQuery); err != nil {
		return dashboard{}, err
	}
	if result.ByPriority, err = d.countBy(ctx, webDashboardPriorityQuery); err != nil {
		return dashboard{}, err
	}
	if result.ByCategory, err = d.countBy(ctx, webDashboardCategoryQuery); err != nil {
		return dashboard{}, err
	}
	for _, item := range result.ByStatus {
		result.Total += item.Count
	}
	return result, nil
}
