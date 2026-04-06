package tg

// Response wraps all Telegram Bot API responses.
type Response[T any] struct {
	OK          bool   `json:"ok"`
	Result      T      `json:"result"`
	Description string `json:"description,omitempty"`
	ErrorCode   int    `json:"error_code,omitempty"`
}

// Update represents an incoming update from Telegram.
type Update struct {
	UpdateID          int      `json:"update_id"`
	Message           *Message `json:"message,omitempty"`
	EditedMessage     *Message `json:"edited_message,omitempty"`
	ChannelPost       *Message `json:"channel_post,omitempty"`
	EditedChannelPost *Message `json:"edited_channel_post,omitempty"`
}

// User represents a Telegram user or bot.
type User struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name,omitempty"`
	UserName  string `json:"username,omitempty"`
	IsBot     bool   `json:"is_bot,omitempty"`
}

// Chat represents a Telegram chat.
type Chat struct {
	ID    int64  `json:"id"`
	Type  string `json:"type"`
	Title string `json:"title,omitempty"`
}

// IsPrivate returns true if the chat is a private chat.
func (c *Chat) IsPrivate() bool {
	return c.Type == "private"
}

// Message represents a Telegram message.
type Message struct {
	MessageID int    `json:"message_id"`
	From      *User  `json:"from,omitempty"`
	Chat      *Chat  `json:"chat,omitempty"`
	Date      int    `json:"date,omitempty"`
	Text      string `json:"text,omitempty"`
	Caption   string `json:"caption,omitempty"`

	Entities        []MessageEntity `json:"entities,omitempty"`
	CaptionEntities []MessageEntity `json:"caption_entities,omitempty"`

	// Reply and quote
	ReplyToMessage *Message   `json:"reply_to_message,omitempty"`
	Quote          *TextQuote `json:"quote,omitempty"`

	// Media
	Photo     []PhotoSize `json:"photo,omitempty"`
	Animation *Animation  `json:"animation,omitempty"`
	Video     *Video      `json:"video,omitempty"`
	Document  *Document   `json:"document,omitempty"`
	Sticker   *Sticker    `json:"sticker,omitempty"`
}

// IsCommand returns true if the message starts with a bot command entity.
func (m *Message) IsCommand() bool {
	if len(m.Entities) == 0 {
		return false
	}
	return m.Entities[0].Offset == 0 && m.Entities[0].Type == "bot_command"
}

// Command returns the command string without the leading slash and without @bot suffix.
func (m *Message) Command() string {
	if !m.IsCommand() {
		return ""
	}
	e := m.Entities[0]
	cmd := m.Text[1:e.Length] // skip leading "/"
	for i, c := range cmd {
		if c == '@' {
			return cmd[:i]
		}
	}
	return cmd
}

// TextQuote contains the quoted part of a replied-to message (Bot API 7.0+).
type TextQuote struct {
	Text     string          `json:"text"`
	Entities []MessageEntity `json:"entities,omitempty"`
	Position int             `json:"position"`
	IsManual bool            `json:"is_manual,omitempty"`
}

// MessageEntity represents a special entity in a text message.
type MessageEntity struct {
	Type   string `json:"type"`
	Offset int    `json:"offset"`
	Length int    `json:"length"`
	URL    string `json:"url,omitempty"`
	User   *User  `json:"user,omitempty"`
}

// PhotoSize represents one size of a photo or file thumbnail.
type PhotoSize struct {
	FileID   string `json:"file_id"`
	FileSize int    `json:"file_size,omitempty"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
}

// Animation represents an animation file (GIF or video without sound).
type Animation struct {
	FileID   string `json:"file_id"`
	MimeType string `json:"mime_type,omitempty"`
	FileSize int    `json:"file_size,omitempty"`
}

// Video represents a video file.
type Video struct {
	FileID   string `json:"file_id"`
	MimeType string `json:"mime_type,omitempty"`
	FileSize int    `json:"file_size,omitempty"`
}

// Document represents a general file.
type Document struct {
	FileID   string `json:"file_id"`
	MimeType string `json:"mime_type,omitempty"`
	FileSize int    `json:"file_size,omitempty"`
}

// Sticker represents a sticker.
type Sticker struct {
	FileID     string `json:"file_id"`
	IsAnimated bool   `json:"is_animated,omitempty"`
	FileSize   int    `json:"file_size,omitempty"`
}

// File represents a file ready to be downloaded.
type File struct {
	FileID   string `json:"file_id"`
	FileSize int    `json:"file_size,omitempty"`
	FilePath string `json:"file_path,omitempty"`
}
