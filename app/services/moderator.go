package services

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"

	"nuclight.org/antispam-tg-bot/pkg/ai"
	e "nuclight.org/antispam-tg-bot/pkg/entities"
	"nuclight.org/antispam-tg-bot/pkg/logger"
)

// ModeratingSrv is a handler of new messages. It decides what to do with a message
// based on the score system for each user. New user receives a default score.
// If score is lower than trusted score, the message is checked for spam. If the
// message is spam, the user receives a penalty score -1 and erase message action is
// returned. If the score reaches ban score, ban action is returned which also erases
// the message. If spam check returns false, score is increased by 1, and noop action
// is returned. When user reaches trusted score, the message is not checked for spam
// anymore.
type ModeratingSrv struct {
	// Log is a logger
	Log logger.Logger

	// DefaultScore is a default score for a new user
	DefaultScore int

	// TrustedScore is a score for a trusted user
	TrustedScore int

	// BanScore is a score for a banned user
	BanScore int

	// ScoreStore is a store for user scores
	ScoreStore ScoreStore

	// MessagesStore is a store for messages
	MessagesStore MessagesStore

	// AI is an AI client
	AI AIClient
}

// HandleMessage handles a message, it takes a message, reviews it and returns an action to be taken
// based on the score system. It returns an action and an error if something goes wrong. Returned
// action has to be considered even if error is not nil.
func (h *ModeratingSrv) HandleMessage(ctx context.Context, msg e.Message) (e.Action, error) {
	score, err := h.ScoreStore.GetScore(ctx, msg.Sender, h.DefaultScore)
	if err != nil {
		return noop, fmt.Errorf("getting user score: %w", err)
	}

	if score >= h.TrustedScore {
		return noop, nil
	}

	if score <= h.BanScore {
		return e.Action{
			Kind: e.ActionKindBan,
			Note: fmt.Sprintf("user score is %d, while ban score is %d", score, h.BanScore),
		}, nil
	}

	messageID, err := h.MessagesStore.SaveMessage(ctx, msg)
	if err != nil {
		return noop, fmt.Errorf("saving message: %w", err)
	}

	isSpam, err := h.checkSpam(ctx, msg.Text)
	if err != nil {
		return noop, fmt.Errorf("checking spam: %w", err)
	}

	if isSpam {
		newScore := score - 1
		var action e.Action
		if newScore <= h.BanScore {
			action = e.Action{
				Kind: e.ActionKindBan,
				Note: "ban score reached",
			}
		} else {
			action = e.Action{
				Kind: e.ActionKindErase,
				Note: "message is a spam",
			}
		}

		err = h.MessagesStore.SaveAction(ctx, messageID, action)
		if err != nil {
			return action, fmt.Errorf("saving action: %w", err)
		}

		err = h.ScoreStore.SetScore(ctx, msg.Sender, newScore)
		if err != nil {
			return action, fmt.Errorf("setting user score: %w", err)
		}

		return action, nil
	}

	newScore := score + 1
	err = h.ScoreStore.SetScore(ctx, msg.Sender, newScore)
	if err != nil {
		return noop, fmt.Errorf("setting user score: %w", err)
	}

	return noop, nil
}

func (h *ModeratingSrv) checkSpam(ctx context.Context, text string) (bool, error) {
	request := &ai.Request{
		Model: ai.DefaultModel,
		Message: []ai.Message{
			{Role: ai.RoleSystem, Content: prompt},
			{Role: ai.RoleUser, Content: text},
		},
		Temperature:    0,
		ResponseFormat: json.RawMessage(ai.YesNoResponseFormat),
	}

	var answer ai.YesNoAnswer
	_, err := h.AI.GetJSONCompletion(ctx, request, &answer)
	if err != nil {
		return false, fmt.Errorf("getting completion: %w", err)
	}

	return answer.Yes, nil
}

type ScoreStore interface {
	GetScore(ctx context.Context, sender e.User, defaultValue int) (int, error)
	SetScore(ctx context.Context, sender e.User, score int) error
}

type MessagesStore interface {
	SaveMessage(ctx context.Context, msg e.Message) (int64, error)
	SaveAction(ctx context.Context, messageID int64, action e.Action) error
}

type AIClient interface {
	GetJSONCompletion(ctx context.Context, request *ai.Request, result any) (*ai.Response, error)
}

var noop = e.Action{
	Kind: e.ActionKindNoop,
	Note: "",
}

//go:embed system_prompt.txt
var prompt string
