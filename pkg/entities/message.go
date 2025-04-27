package entities

type User struct {
	Source    Source
	ID        string
	ChatID    string
	ChatTitle string
}

type Message struct {
	Sender User
	ID     string
	Text   string
}

type Source string

const (
	SourceTelegram = "telegram"
)
