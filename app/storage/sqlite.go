package storage

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
	e "nuclight.org/antispam-tg-bot/pkg/entities"
)

type SQLite struct {
	db *sql.DB
}

func NewSQLite(ctx context.Context, filePath string) (*SQLite, error) {
	db, err := sql.Open("sqlite3", filePath)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite3 database: %w", err)
	}

	client := &SQLite{
		db: db,
	}

	err = client.init(ctx)
	if err != nil {
		return nil, fmt.Errorf("initializing sqlite3 database: %w", err)
	}

	return client, nil
}

func (c *SQLite) Close() error {
	return c.db.Close()
}

func (c *SQLite) GetScore(ctx context.Context, user e.User, defaultValue int) (int, error) {
	var score int
	err := c.db.QueryRowContext(
		ctx,
		"SELECT score FROM scores WHERE chat_id = ? and user_id = ?",
		user.ChatID, user.ID,
	).Scan(&score)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return defaultValue, nil
		}

		return 0, err
	}

	return score, nil
}

func (c *SQLite) SetScore(ctx context.Context, user e.User, score int) error {
	_, err := c.db.ExecContext(
		ctx,
		`INSERT INTO scores (chat_id, user_id, user_name, score, updated_at)
			VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP) 
			ON CONFLICT(chat_id, user_id) DO UPDATE 
			    SET score = ?, updated_at = CURRENT_TIMESTAMP`,
		user.ChatID, user.ID, user.Name, score, score,
	)
	return err
}

func (c *SQLite) SaveMessage(ctx context.Context, msg e.Message) (int64, error) {
	_, err := c.db.ExecContext(
		ctx,
		`INSERT INTO chats (
			chat_id, title, created_at
		) VALUES (
			?, ?, CURRENT_TIMESTAMP
		) ON CONFLICT(chat_id) DO UPDATE SET title = ?`,
		msg.Sender.ChatID, msg.Sender.ChatTitle, msg.Sender.ChatTitle,
	)
	if err != nil {
		return 0, fmt.Errorf("inserting chat: %w", err)
	}

	result, err := c.db.ExecContext(
		ctx,
		`INSERT INTO messages (
			message_id, chat_id, sender_user_id, sender_user_name, text, created_at, action, action_note
		) VALUES (
			?, ?, ?, ?, ?, CURRENT_TIMESTAMP, NULL, NULL
		)`,
		msg.ID, msg.Sender.ChatID, msg.Sender.ID, msg.Sender.Name, msg.Text,
	)
	if err != nil {
		return 0, fmt.Errorf("inserting message: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("getting last insert id: %w", err)
	}

	return id, nil
}

func (c *SQLite) ListMessages(ctx context.Context, limit int) ([]e.SavedMessage, error) {
	rows, err := c.db.QueryContext(
		ctx,
		`SELECT m.id, m.message_id, m.chat_id, m.sender_user_id, m.sender_user_name, m.text, 
		        m.created_at, m.action, m.action_note, m.error
		 FROM messages AS m
		 ORDER BY m.created_at DESC
		 LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("querying messages: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var messages []e.SavedMessage
	for rows.Next() {
		var msg e.SavedMessage
		err = rows.Scan(
			&msg.ID,
			&msg.Sender.ID,
			&msg.Sender.ChatID,
			&msg.Sender.ID,
			&msg.Sender.Name,
			&msg.Text,
			&msg.CreatedAt,
			&msg.Action,
			&msg.ActionNote,
			&msg.Error,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning message: %w", err)
		}
		messages = append(messages, msg)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating over messages: %w", err)
	}

	return messages, nil

}

func (c *SQLite) SaveAction(ctx context.Context, messageID int64, action e.Action) error {
	_, err := c.db.ExecContext(
		ctx,
		`UPDATE messages SET action = ?, action_note = ? WHERE id = ?`,
		string(action.Kind),
		action.Note,
		messageID,
	)
	return err
}

func (c *SQLite) SaveError(ctx context.Context, messageID int64, error string) error {
	_, err := c.db.ExecContext(
		ctx,
		`UPDATE messages SET error = ? WHERE id = ?`,
		error,
		messageID,
	)
	return err
}

//go:embed init.sql
var initQuery string

func (c *SQLite) init(ctx context.Context) error {
	_, err := c.db.ExecContext(ctx, initQuery)
	return err
}
