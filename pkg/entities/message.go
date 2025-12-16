package entities

import "time"

type User struct {
	ID        string
	Name      string
	ChatID    string
	ChatTitle string
}

type Message struct {
	Sender      User
	ID          string
	Text        string
	MediaType   *string // MIME type, nil if no attachment
	MediaFileID *string // Telegram file ID (permanent, used for on-demand download)
	MediaSize   *int64  // Original size in bytes
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
	MediaFileID    *string // Telegram file ID (new)
	MediaContent   []byte  // Deprecated: kept for backwards compat with old data
	MediaSize      *int64
	MediaTruncated bool // Deprecated: kept for backwards compat with old data
}

func (m *Message) HasText() bool {
	return m.Text != ""
}

func (m *Message) HasMedia() bool {
	return m.MediaType != nil
}
