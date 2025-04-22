package entities

type Message struct {
	Source       Source
	ID           string
	ChatID       string
	SenderUserID string
	Text         string
}

type Source string

const (
	SourceTelegram = "telegram"
)
