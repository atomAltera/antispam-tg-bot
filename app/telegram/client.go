package telegram

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"

	"github.com/getsentry/sentry-go"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	e "nuclight.org/antispam-tg-bot/pkg/entities"
	"nuclight.org/antispam-tg-bot/pkg/logger"
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

	bot *tgbotapi.BotAPI
	wg  sync.WaitGroup
}

func (c *Client) Start(ctx context.Context) (err error) {
	if c.WorkersNum == 0 {
		return fmt.Errorf("workers number must be greater than 0")
	}

	log := c.Log

	c.bot, err = tgbotapi.NewBotAPI(c.APIToken)
	if err != nil {
		return fmt.Errorf("creating bot api: %w", err)
	}

	log.Info("bot api created", "username", c.bot.Self.UserName)

	tgUpdatesConf := tgbotapi.NewUpdate(0)
	tgUpdatesConf.Timeout = 60

	tgUpdatesChan := c.bot.GetUpdatesChan(tgUpdatesConf)

	for i := 0; i < c.WorkersNum; i++ {
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			c.handleUpdatesFromChan(ctx, tgUpdatesChan)
		}()
	}

	return nil
}

func (c *Client) Wait() {
	c.wg.Wait()
}

func (c *Client) handleUpdatesFromChan(ctx context.Context, tgUpdatesChan tgbotapi.UpdatesChannel) {
	for {
		select {
		case <-ctx.Done():
			return
		case tgUpdate := <-tgUpdatesChan:
			err := c.handleUpdate(ctx, tgUpdate)
			if err != nil {
				c.Log.Error("handling update", "tg_update_id", tgUpdate.UpdateID, "error", err)
				sentry.CaptureException(err)
			}
		}
	}
}

func (c *Client) handleUpdate(ctx context.Context, tgUpdate tgbotapi.Update) error {
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
		mimeType, fileID, size, err := c.getMediaMetadata(mi)
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

func takeText(msg *tgbotapi.Message) string {
	if msg.Text != "" {
		return msg.Text
	}

	if msg.Caption != "" {
		return msg.Caption
	}

	return ""
}

func (c *Client) applyAction(ctx context.Context, tgUpdateID int, tgMsg *tgbotapi.Message, act e.Action) error {
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

func (c *Client) eraseMessage(_ context.Context, tgMsg *tgbotapi.Message) error {
	conf := tgbotapi.NewDeleteMessage(tgMsg.Chat.ID, tgMsg.MessageID)
	_, err := c.bot.Request(conf)
	return err
}

func (c *Client) banUser(_ context.Context, userID int64, chatID int64) error {
	conf := tgbotapi.BanChatMemberConfig{
		ChatMemberConfig: tgbotapi.ChatMemberConfig{
			ChatID: chatID,
			UserID: userID,
		},
		UntilDate:      0,
		RevokeMessages: false,
	}
	_, err := c.bot.Request(conf)
	return err
}

func (c *Client) replyPrivate(_ context.Context, tgMsg *tgbotapi.Message) error {
	msg := tgbotapi.NewMessage(
		tgMsg.Chat.ID,
		"Hello, I can help you with spam moderation in your group.\n"+
			"Please add me to your group as admin with ability to delete messages",
	)

	msg.ParseMode = tgbotapi.ModeHTML
	msg.DisableWebPagePreview = true

	_, err := c.bot.Send(msg)
	return err
}

func takeMessage(update tgbotapi.Update) *tgbotapi.Message {
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

func takeMessageID(message *tgbotapi.Message) string {
	return strconv.Itoa(message.MessageID)
}

func takeChatID(chat *tgbotapi.Chat) string {
	return strconv.FormatInt(chat.ID, 10)
}

func takeUserID(user *tgbotapi.User) string {
	return strconv.FormatInt(user.ID, 10)
}

func takeUserName(user *tgbotapi.User) string {
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

func getMediaInfo(msg *tgbotapi.Message) *mediaInfo {
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
func (c *Client) getMediaMetadata(info *mediaInfo) (mimeType *string, fileID *string, size *int64, err error) {
	file, err := c.bot.GetFile(tgbotapi.FileConfig{FileID: info.fileID})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("getting file info: %w", err)
	}

	fileSize := int64(file.FileSize)
	return &info.mimeType, &info.fileID, &fileSize, nil
}

// DownloadFile downloads file content by file ID (on-demand)
func (c *Client) DownloadFile(ctx context.Context, fileID string) ([]byte, error) {
	file, err := c.bot.GetFile(tgbotapi.FileConfig{FileID: fileID})
	if err != nil {
		return nil, fmt.Errorf("getting file: %w", err)
	}

	fileURL := file.Link(c.bot.Token)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("downloading file: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}

	return content, nil
}
