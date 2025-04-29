package entities

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

func (m *Message) HasText() bool {
	return m.Text != ""
}
