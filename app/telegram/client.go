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

	updatesConf := tgbotapi.NewUpdate(0)
	updatesConf.Timeout = 60

	updatesChan := c.bot.GetUpdatesChan(updatesConf)

	for i := 0; i < c.WorkersNum; i++ {
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			c.handleUpdatesFromChan(ctx, updatesChan)
		}()
	}

	return nil
}

func (c *Client) Wait() {
	c.wg.Wait()
}

func (c *Client) handleUpdatesFromChan(ctx context.Context, updatesChan tgbotapi.UpdatesChannel) {
	for {
		select {
		case <-ctx.Done():
			return
		case update := <-updatesChan:
			err := c.handleUpdate(ctx, update)
			if err != nil {
				c.Log.Error("handling update", "tg_update_id", update.UpdateID, "error", err)
			}
		}
	}
}

func (c *Client) handleUpdate(ctx context.Context, update tgbotapi.Update) error {
	log := c.Log.With("tg_update_id", update.UpdateID)

	defer func() {
		if err := recover(); err != nil {
			log.Error("panic", "error", err)
		}
	}()

	if update.Message == nil {
		log.Warn("message is nil")
		return nil
	}

	if update.Message.From == nil {
		log.Warn("message from is nil")
		return nil
	}

	if update.Message.Chat == nil {
		log.Warn("message chat is nil")
		return nil
	}

	if update.Message.Chat.IsPrivate() {
		log.Info("message is private")
		err := c.replyPrivate(ctx, update)
		if err != nil {
			log.Error("replying to private message", "error", err)
		}
		return nil
	}

	log.Info(
		"new message",
		"tg_message_id", update.Message.MessageID,
		"tg_user_id", update.Message.From.ID,
		"tg_user_nick", update.Message.From.UserName,
		"tg_user_fist_name", update.Message.From.FirstName,
		"tg_user_last_name", update.Message.From.LastName,
		"tg_chat_id", update.Message.Chat.ID,
		"tg_chat_title", update.Message.Chat.Title,
		"text", update.Message.Text,
	)

	if update.Message.IsCommand() {
		// TODO: handle commands
		log.Info("command received", "command")
		return nil
	}

	msg := e.Message{
		Sender: e.User{
			ID:        takeUserID(update.Message.From),
			Name:      takeUserName(update.Message.From),
			ChatID:    takeChatID(update.Message.Chat),
			ChatTitle: update.Message.Chat.Title,
		},
		ID:   takeMessageID(update.Message),
		Text: update.Message.Text,
	}

	act, err := c.Handler.HandleMessage(ctx, msg)
	if err != nil {
		return fmt.Errorf("handling message: %w", err)
	}

	log.Info("message handled", "action", act.Kind, "note", act.Note)
	err = c.applyAction(ctx, update, act)
	if err != nil {
		return fmt.Errorf("applying action: %w", err)
	}

	return nil

}

func (c *Client) applyAction(ctx context.Context, update tgbotapi.Update, act e.Action) error {
	log := c.Log.With("tg_update_id", update.UpdateID)

	switch act.Kind {
	case e.ActionKindNoop:
		return nil
	case e.ActionKindErase:
		log.Info("erasing message")

		err := c.eraseMessage(ctx, update)
		if err != nil {
			return fmt.Errorf("erasing message: %w", err)
		}

		return nil
	case e.ActionKindBan:
		log.Info("erasing message")

		err := c.eraseMessage(ctx, update)
		if err != nil {
			return fmt.Errorf("erasing message: %w", err)
		}

		return nil

	default:
		return fmt.Errorf("unknown action kind: %s", act.Kind)
	}

}

func (c *Client) eraseMessage(_ context.Context, update tgbotapi.Update) error {
	conf := tgbotapi.NewDeleteMessage(update.Message.Chat.ID, update.Message.MessageID)
	_, err := c.bot.Request(conf)
	return err
}

func (c *Client) replyPrivate(_ context.Context, update tgbotapi.Update) error {
	msg := tgbotapi.NewMessage(
		update.Message.Chat.ID,
		"Hello, I can help you with spam moderation in your group.\n"+
			"Please add me to your group as admin with ability to delete messages",
	)

	msg.ParseMode = tgbotapi.ModeHTML
	msg.DisableWebPagePreview = true

	_, err := c.bot.Send(msg)
	return err
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
