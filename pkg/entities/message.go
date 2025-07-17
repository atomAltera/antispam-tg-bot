package entities

import "time"

type User struct {
	ID        string
	Name      string
	ChatID    string
	ChatTitle string
}

type Message struct {
	Sender User
	ID     string
	Text   string
}

type SavedMessage struct {
	Sender     User
	ID         string
	Text       string
	CreatedAt  time.Time
	Action     *ActionKind
	ActionNote *string
	Error      *string
}

func (m *Message) HasText() bool {
	return m.Text != ""
}
