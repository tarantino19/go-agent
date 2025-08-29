package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	_ "github.com/mattn/go-sqlite3"
)

type Database struct {
	db *sql.DB
}

type UsageRow struct {
	Date         string
	Model        string
	InputTokens  int64
	OutputTokens int64
	TotalTokens  int64
}

type Session struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Message struct {
	ID        int       `json:"id"`
	SessionID int       `json:"session_id"`
	Role      string    `json:"role"` // "user" or "assistant"
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

func NewDatabase(dbPath string) (*Database, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	database := &Database{db: db}
	if err := database.initSchema(); err != nil {
		return nil, fmt.Errorf("failed to initialize database schema: %w", err)
	}

	return database, nil
}

func (d *Database) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS sessions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id INTEGER NOT NULL,
		role TEXT NOT NULL, -- 'user' or 'assistant'
		content TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (session_id) REFERENCES sessions(id)
	);

	-- Track daily token usage per model
	CREATE TABLE IF NOT EXISTS usage (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		date TEXT NOT NULL, -- YYYY-MM-DD
		model TEXT NOT NULL,
		input_tokens INTEGER NOT NULL DEFAULT 0,
		output_tokens INTEGER NOT NULL DEFAULT 0,
		total_tokens INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(date, model)
	);

	CREATE INDEX IF NOT EXISTS idx_messages_session_id ON messages(session_id);
	CREATE INDEX IF NOT EXISTS idx_sessions_updated_at ON sessions(updated_at);
	CREATE INDEX IF NOT EXISTS idx_usage_date ON usage(date);
	`

	_, err := d.db.Exec(schema)
	return err
}

func (d *Database) CreateSession(name string) (*Session, error) {
	if name == "" {
		name = fmt.Sprintf("Session %s", time.Now().Format("2006-01-02 15:04:05"))
	}

	result, err := d.db.Exec(
		"INSERT INTO sessions (name) VALUES (?)",
		name,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get session ID: %w", err)
	}

	return d.GetSession(int(id))
}

func (d *Database) GetSession(sessionID int) (*Session, error) {
	var session Session
	err := d.db.QueryRow(
		"SELECT id, name, created_at, updated_at FROM sessions WHERE id = ?",
		sessionID,
	).Scan(&session.ID, &session.Name, &session.CreatedAt, &session.UpdatedAt)

	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	return &session, nil
}

func (d *Database) ListSessions() ([]Session, error) {
	rows, err := d.db.Query(
		"SELECT id, name, created_at, updated_at FROM sessions ORDER BY updated_at DESC",
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions: %w", err)
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var session Session
		err := rows.Scan(&session.ID, &session.Name, &session.CreatedAt, &session.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan session: %w", err)
		}
		sessions = append(sessions, session)
	}

	return sessions, nil
}

func (d *Database) SaveMessage(sessionID int, role, content string) error {
	_, err := d.db.Exec(
		"INSERT INTO messages (session_id, role, content) VALUES (?, ?, ?)",
		sessionID, role, content,
	)
	if err != nil {
		return fmt.Errorf("failed to save message: %w", err)
	}

	// Update session's updated_at timestamp
	_, err = d.db.Exec(
		"UPDATE sessions SET updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		sessionID,
	)
	if err != nil {
		return fmt.Errorf("failed to update session timestamp: %w", err)
	}

	return nil
}

func (d *Database) GetSessionMessages(sessionID int) ([]Message, error) {
	rows, err := d.db.Query(
		"SELECT id, session_id, role, content, created_at FROM messages WHERE session_id = ? ORDER BY created_at ASC",
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get session messages: %w", err)
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var message Message
		err := rows.Scan(&message.ID, &message.SessionID, &message.Role, &message.Content, &message.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan message: %w", err)
		}
		messages = append(messages, message)
	}

	return messages, nil
}

func (d *Database) DeleteSession(sessionID int) error {
	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Delete messages first (foreign key constraint)
	_, err = tx.Exec("DELETE FROM messages WHERE session_id = ?", sessionID)
	if err != nil {
		return fmt.Errorf("failed to delete session messages: %w", err)
	}

	// Delete session
	_, err = tx.Exec("DELETE FROM sessions WHERE id = ?", sessionID)
	if err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}

	return tx.Commit()
}

func (d *Database) RenameSession(sessionID int, newName string) error {
	_, err := d.db.Exec(
		"UPDATE sessions SET name = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		newName, sessionID,
	)
	if err != nil {
		return fmt.Errorf("failed to rename session: %w", err)
	}
	return nil
}

func (d *Database) Close() error {
	return d.db.Close()
}

// Upsert daily usage by model
func (d *Database) AddDailyUsage(date, model string, inputTokens, outputTokens int64) error {
	// total computation happens atomically in SQL
	_, err := d.db.Exec(`
		INSERT INTO usage (date, model, input_tokens, output_tokens, total_tokens)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(date, model) DO UPDATE SET
			input_tokens = input_tokens + excluded.input_tokens,
			output_tokens = output_tokens + excluded.output_tokens,
			total_tokens = total_tokens + excluded.total_tokens
	`, date, model, inputTokens, outputTokens, inputTokens+outputTokens)
	if err != nil {
		return fmt.Errorf("failed to upsert usage: %w", err)
	}
	return nil
}

// List usage entries optionally filtered by date or model
func (d *Database) ListUsage(date string) ([]UsageRow, error) {
	query := "SELECT date, model, input_tokens, output_tokens, total_tokens FROM usage"
	args := []any{}
	if date != "" {
		query += " WHERE date = ?"
		args = append(args, date)
	}
	query += " ORDER BY date DESC, model ASC"
	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query usage: %w", err)
	}
	defer rows.Close()
	var out []UsageRow
	for rows.Next() {
		var u UsageRow
		if err := rows.Scan(&u.Date, &u.Model, &u.InputTokens, &u.OutputTokens, &u.TotalTokens); err != nil {
			return nil, fmt.Errorf("failed to scan usage: %w", err)
		}
		out = append(out, u)
	}
	return out, nil
}

// Helper functions to convert between anthropic messages and database format
func ConvertAnthropicMessagesToDB(messages []anthropic.MessageParam) []Message {
	var dbMessages []Message

	for _, msg := range messages {
		role := string(msg.Role)

		// Convert content to JSON string for storage
		contentBytes, _ := json.Marshal(msg.Content)
		content := string(contentBytes)

		dbMessages = append(dbMessages, Message{
			Role:    role,
			Content: content,
		})
	}

	return dbMessages
}

func ConvertDBMessagesToAnthropic(dbMessages []Message) ([]anthropic.MessageParam, error) {
	var messages []anthropic.MessageParam

	for _, dbMsg := range dbMessages {
		var content []anthropic.ContentBlockParamUnion

		// Parse JSON content back to anthropic format
		err := json.Unmarshal([]byte(dbMsg.Content), &content)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal message content: %w", err)
		}

		message := anthropic.MessageParam{
			Role:    anthropic.MessageParamRole(dbMsg.Role),
			Content: content,
		}

		messages = append(messages, message)
	}

	return messages, nil
}
