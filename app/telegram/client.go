package telegram

import (
	"context"
	"fmt"
	"runtime/debug"
	"time"
	"strconv"
	"strings"
	"sync"

	"github.com/getsentry/sentry-go"
	e "nuclight.org/antispam-tg-bot/pkg/entities"
	"nuclight.org/antispam-tg-bot/pkg/logger"
	"nuclight.org/antispam-tg-bot/pkg/tg"
)

type MessageHandler interface {
	HandleMessage(ctx context.Context, msg e.Message) (e.Action, error)
}

type Client struct {
	Log        logger.Logger
	APIToken   string
	WorkersNum int
	DevMode    bool
	Handler    MessageHandler

	api         *tg.Client
	updatesChan chan tg.Update
	wg          sync.WaitGroup
}

func (c *Client) Start(ctx context.Context) (err error) {
	if c.WorkersNum == 0 {
		return fmt.Errorf("workers number must be greater than 0")
	}

	log := c.Log

	c.api = tg.NewClient(c.APIToken, nil)

	me, err := c.api.GetMe(ctx)
	if err != nil {
		return fmt.Errorf("getting bot info: %w", err)
	}

	log.Info("bot api created", "username", me.UserName)

	c.updatesChan = make(chan tg.Update)

	c.wg.Add(1)
	go c.pollUpdates(ctx)

	for i := 0; i < c.WorkersNum; i++ {
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			c.handleUpdatesFromChan(ctx)
		}()
	}

	return nil
}

func (c *Client) Wait() {
	c.wg.Wait()
}

func (c *Client) pollUpdates(ctx context.Context) {
	defer c.wg.Done()
	offset := 0
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		updates, err := c.api.GetUpdates(ctx, offset, 60)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			c.Log.Error("getting updates", "error", err)
			time.Sleep(5 * time.Second)
			continue
		}
		for _, update := range updates {
			offset = update.UpdateID + 1
			select {
			case c.updatesChan <- update:
			case <-ctx.Done():
				return
			}
		}
	}
}

func (c *Client) handleUpdatesFromChan(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case tgUpdate := <-c.updatesChan:
			err := c.handleUpdate(ctx, tgUpdate)
			if err != nil {
				c.Log.Error("handling update", "tg_update_id", tgUpdate.UpdateID, "error", err)
			}
		}
	}
}

func (c *Client) handleUpdate(ctx context.Context, tgUpdate tg.Update) error {
	log := c.Log.With("tg_update_id", tgUpdate.UpdateID)

	defer func() {
		if err := recover(); err != nil {
			log.Error("panic", "error", err, "stack", string(debug.Stack()))
			sentry.CurrentHub().Recover(err)
		}
	}()

	tgMsg := takeMessage(tgUpdate)
	if tgMsg == nil {
		log.Warn("message is nil")
		return nil
	}

	if tgMsg.From == nil {
		log.Warn("message from is nil")
		return nil
	}

	if tgMsg.Chat == nil {
		log.Warn("message chat is nil")
		return nil
	}

	if tgMsg.Chat.IsPrivate() && !c.DevMode {
		log.Info("message is private")
		err := c.replyPrivate(ctx, tgMsg)
		if err != nil {
			log.Error("replying to private message", "error", err)
		}
		return nil
	}

	log.Info(
		"new message",
		"tg_message_id", tgMsg.MessageID,
		"tg_user_id", tgMsg.From.ID,
		"tg_user_nick", tgMsg.From.UserName,
		"tg_user_fist_name", tgMsg.From.FirstName,
		"tg_user_last_name", tgMsg.From.LastName,
		"tg_chat_id", tgMsg.Chat.ID,
		"tg_chat_title", tgMsg.Chat.Title,
		"text", takeText(tgMsg),
	)

	if tgMsg.IsCommand() {
		// TODO: handle commands
		log.Info("command received", "command", tgMsg.Command())
		return nil
	}

	msg := e.Message{
		Sender: e.User{
			ID:        takeUserID(tgMsg.From),
			Name:      takeUserName(tgMsg.From),
			ChatID:    takeChatID(tgMsg.Chat),
			ChatTitle: tgMsg.Chat.Title,
		},
		ID:   takeMessageID(tgMsg),
		Text: takeText(tgMsg),
	}

	if mi := getMediaInfo(tgMsg); mi != nil {
		mimeType, fileID, size, err := c.getMediaMetadata(ctx, mi)
		if err != nil {
			log.Error("getting media metadata", "error", err)
		} else {
			msg.MediaType = mimeType
			msg.MediaFileID = fileID
			msg.MediaSize = size
		}
	}

	act, err := c.Handler.HandleMessage(ctx, msg)
	if err != nil {
		return fmt.Errorf("handling message: %w", err)
	}

	log.Info("message handled", "action", act.Kind, "note", act.Note)
	err = c.applyAction(ctx, tgUpdate.UpdateID, tgMsg, act)
	if err != nil {
		return fmt.Errorf("applying action: %w", err)
	}

	return nil

}

func takeText(msg *tg.Message) string {
	text := msg.Text
	if text == "" {
		text = msg.Caption
	}

	// Extract quoted text from reply (Bot API 7.0+ TextQuote)
	if msg.Quote != nil && msg.Quote.Text != "" {
		text = appendQuoted(text, msg.Quote.Text)
	} else if msg.ReplyToMessage != nil {
		// Fallback: use the full reply message text
		quoted := msg.ReplyToMessage.Text
		if quoted == "" {
			quoted = msg.ReplyToMessage.Caption
		}
		if quoted != "" {
			text = appendQuoted(text, quoted)
		}
	}

	return text
}

func appendQuoted(text, quoted string) string {
	if text != "" {
		return text + "\n\n[quoted message]:\n" + quoted
	}
	return "[quoted message]:\n" + quoted
}

func (c *Client) applyAction(ctx context.Context, tgUpdateID int, tgMsg *tg.Message, act e.Action) error {
	log := c.Log.With("tg_update_id", tgUpdateID)

	switch act.Kind {
	case e.ActionKindNoop:
		return nil
	case e.ActionKindErase:
		log.Info("erasing message")

		err := c.eraseMessage(ctx, tgMsg)
		if err != nil {
			return fmt.Errorf("erasing message: %w", err)
		}

		return nil
	case e.ActionKindBan:
		log.Info("erasing message")
		if err := c.eraseMessage(ctx, tgMsg); err != nil {
			return fmt.Errorf("erasing message: %w", err)
		}

		log.Info("banning user", "tg_user_id", tgMsg.From.ID, "tg_chat_id", tgMsg.Chat.ID, "tg_chat_title", tgMsg.Chat.Title, "tg_user_name", takeUserName(tgMsg.From))
		if err := c.banUser(ctx, tgMsg.From.ID, tgMsg.Chat.ID); err != nil {
			return fmt.Errorf("banning user: %w", err)
		}

		return nil

	default:
		return fmt.Errorf("unknown action kind: %s", act.Kind)
	}

}

func (c *Client) eraseMessage(ctx context.Context, tgMsg *tg.Message) error {
	return c.api.DeleteMessage(ctx, tgMsg.Chat.ID, tgMsg.MessageID)
}

func (c *Client) banUser(ctx context.Context, userID int64, chatID int64) error {
	return c.api.BanChatMember(ctx, chatID, userID)
}

func (c *Client) replyPrivate(ctx context.Context, tgMsg *tg.Message) error {
	return c.api.SendMessage(ctx, tgMsg.Chat.ID,
		"Hello, I can help you with spam moderation in your group.\n"+
			"Please add me to your group as admin with ability to delete messages")
}

func takeMessage(update tg.Update) *tg.Message {
	if update.Message != nil {
		return update.Message
	}

	if update.EditedMessage != nil {
		return update.EditedMessage
	}

	if update.ChannelPost != nil {
		return update.ChannelPost
	}

	if update.EditedChannelPost != nil {
		return update.EditedChannelPost
	}

	return nil
}

func takeMessageID(message *tg.Message) string {
	return strconv.Itoa(message.MessageID)
}

func takeChatID(chat *tg.Chat) string {
	return strconv.FormatInt(chat.ID, 10)
}

func takeUserID(user *tg.User) string {
	return strconv.FormatInt(user.ID, 10)
}

func takeUserName(user *tg.User) string {
	var sb strings.Builder

	if user.FirstName != "" {
		sb.WriteString(user.FirstName)
	}

	if user.LastName != "" {
		if sb.Len() > 0 {
			sb.WriteRune(' ')
		}
		sb.WriteString(user.LastName)
	}

	if user.UserName != "" {
		if sb.Len() > 0 {
			sb.WriteRune(' ')
			sb.WriteRune('(')
			sb.WriteRune('@')
			sb.WriteString(user.UserName)
			sb.WriteRune(')')
		} else {
			sb.WriteRune('@')
			sb.WriteString(user.UserName)
		}
	}

	if sb.Len() == 0 {
		return takeUserID(user)
	}

	return sb.String()
}

type mediaInfo struct {
	fileID   string
	mimeType string
}

func getMediaInfo(msg *tg.Message) *mediaInfo {
	if len(msg.Photo) > 0 {
		// Get largest photo (last in array)
		photo := msg.Photo[len(msg.Photo)-1]
		return &mediaInfo{fileID: photo.FileID, mimeType: "image/jpeg"}
	}
	if msg.Animation != nil {
		return &mediaInfo{fileID: msg.Animation.FileID, mimeType: msg.Animation.MimeType}
	}
	if msg.Video != nil {
		return &mediaInfo{fileID: msg.Video.FileID, mimeType: msg.Video.MimeType}
	}
	if msg.Document != nil {
		return &mediaInfo{fileID: msg.Document.FileID, mimeType: msg.Document.MimeType}
	}
	if msg.Sticker != nil {
		mimeType := "image/webp"
		if msg.Sticker.IsAnimated {
			mimeType = "application/x-tgsticker"
		}
		return &mediaInfo{fileID: msg.Sticker.FileID, mimeType: mimeType}
	}
	return nil
}

// getMediaMetadata returns metadata about the media without downloading content
func (c *Client) getMediaMetadata(ctx context.Context, info *mediaInfo) (mimeType *string, fileID *string, size *int64, err error) {
	file, err := c.api.GetFile(ctx, info.fileID)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("getting file info: %w", err)
	}

	fileSize := int64(file.FileSize)
	return &info.mimeType, &info.fileID, &fileSize, nil
}

// DownloadFile downloads file content by file ID (on-demand)
func (c *Client) DownloadFile(ctx context.Context, fileID string) ([]byte, error) {
	return c.api.DownloadFile(ctx, fileID)
}
