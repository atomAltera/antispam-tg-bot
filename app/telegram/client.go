package telegram

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

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
			}
		}
	}
}

func (c *Client) handleUpdate(ctx context.Context, tgUpdate tgbotapi.Update) error {
	log := c.Log.With("tg_update_id", tgUpdate.UpdateID)

	defer func() {
		if err := recover(); err != nil {
			log.Error("panic", "error", err)
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
		"text", tgMsg.Text,
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
		Text: tgMsg.Text,
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

		err := c.eraseMessage(ctx, tgMsg)
		if err != nil {
			return fmt.Errorf("erasing message: %w", err)
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
