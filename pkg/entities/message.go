package entities

import "time"

type User struct {
	ID        string
	Name      string
	ChatID    string
	ChatTitle string
}

type Message struct {
	Sender         User
	ID             string
	Text           string
	MediaType      *string // MIME type, nil if no attachment
	MediaContent   []byte  // Binary content, nil if truncated or no attachment
	MediaSize      *int64  // Original size in bytes
	MediaTruncated bool    // True if content > 1MB and not stored
}

type SavedMessage struct {
	Sender         User
	ID             string
	Text           string
	CreatedAt      time.Time
	Action         *ActionKind
	ActionNote     *string
	Error          *string
	MediaType      *string
	MediaContent   []byte
	MediaSize      *int64
	MediaTruncated bool
}

func (m *Message) HasText() bool {
	return m.Text != ""
}

func (m *Message) HasMedia() bool {
	return m.MediaType != nil
}
