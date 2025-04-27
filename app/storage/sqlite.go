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
		"SELECT score FROM scores WHERE source = ? AND chat_id = ? and user_id = ?",
		user.Source, user.ChatID, user.ID,
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
		`INSERT INTO scores (source, chat_id, user_id, score, updated_at)
			VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP) 
			ON CONFLICT(source, chat_id, user_id) DO UPDATE 
			    SET score = ?, updated_at = CURRENT_TIMESTAMP`,
		user.Source, user.ChatID, user.ID, score, score,
	)
	return err
}

func (c *SQLite) SaveMessage(ctx context.Context, msg e.Message) (int64, error) {
	_, err := c.db.ExecContext(
		ctx,
		`INSERT INTO chats (
			source, chat_id, title, created_at
		) VALUES (
			?, ?, ?, CURRENT_TIMESTAMP
		) ON CONFLICT(source, chat_id) DO UPDATE SET title = ?`,
		msg.Sender.Source, msg.Sender.ChatID, msg.Sender.ChatTitle, msg.Sender.ChatTitle,
	)
	if err != nil {
		return 0, fmt.Errorf("inserting chat: %w", err)
	}

	result, err := c.db.ExecContext(
		ctx,
		`INSERT INTO messages (
			source, message_id, chat_id, sender_user_id, text, created_at, action, action_note
		) VALUES (
			?, ?, ?, ?, ?, CURRENT_TIMESTAMP, NULL, NULL
		)`,
		msg.Sender.Source, msg.ID, msg.Sender.ChatID, msg.Sender.ID, msg.Text,
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

//go:embed init.sql
var initQuery string

func (c *SQLite) init(ctx context.Context) error {
	_, err := c.db.ExecContext(ctx, initQuery)
	return err
}
