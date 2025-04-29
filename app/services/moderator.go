package services

import (
	"context"
	_ "embed"
	"fmt"

	"nuclight.org/antispam-tg-bot/pkg/ai"
	e "nuclight.org/antispam-tg-bot/pkg/entities"
)

// ModeratingSrv handles new messages by determining appropriate actions based on a user score system.
// Each new user receives a default score. If a user's score is below the trusted threshold,
// their messages are checked for spam. When spam is detected, the user receives a penalty (-1 score)
// and the message is erased. If a user's score falls to or below the ban threshold, a ban action
// is returned which also erases the message. If a message passes the spam check, the user's score
// increases by 1 and no action is taken. Once a user reaches the trusted score threshold, their
// messages are no longer checked for spam.
type ModeratingSrv struct {
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
func (s *ModeratingSrv) HandleMessage(ctx context.Context, msg e.Message) (e.Action, error) {
	if has := msg.HasText(); !has {
		// TODO: support non-text messages
		return noop, nil
	}

	score, err := s.ScoreStore.GetScore(ctx, msg.Sender, s.DefaultScore)
	if err != nil {
		return noop, fmt.Errorf("getting user score: %w", err)
	}

	if score >= s.TrustedScore {
		return noop, nil
	}

	messageID, err := s.MessagesStore.SaveMessage(ctx, msg)
	if err != nil {
		return noop, fmt.Errorf("saving message: %w", err)
	}

	action, delta, err := s.getAction(ctx, score, msg)
	if err != nil {
		_ = s.MessagesStore.SaveError(ctx, messageID, err.Error())
		return action, fmt.Errorf("getting action: %w", err)
	}

	err = s.MessagesStore.SaveAction(ctx, messageID, action)
	if err != nil {
		return action, fmt.Errorf("saving action: %w", err)
	}

	newScore := s.getNewScore(score, delta)
	if newScore != score {
		err = s.ScoreStore.SetScore(ctx, msg.Sender, newScore)
		if err != nil {
			return action, fmt.Errorf("setting user score: %w", err)
		}
	}

	return action, nil
}

func (s *ModeratingSrv) getAction(ctx context.Context, score int, msg e.Message) (e.Action, int, error) {
	report, err := s.checkSpam(ctx, msg.Text)
	if err != nil {
		return noop, 0, fmt.Errorf("checking spam: %w", err)
	}

	if !report.IsSpam {
		return noop, 1, nil
	}

	newScore := s.getNewScore(score, -1)
	if newScore <= s.BanScore {
		return e.Action{
			Kind: e.ActionKindBan,
			Note: report.Note,
		}, -1, nil
	}

	return e.Action{
		Kind: e.ActionKindErase,
		Note: report.Note,
	}, -1, nil
}

func (s *ModeratingSrv) checkSpam(ctx context.Context, text string) (ai.SpamCheck, error) {
	var check ai.SpamCheck
	_, err := s.AI.GetJSONCompletion(ctx, prompt, text, ai.SpamCheckFormat, &check)
	if err != nil {
		return check, fmt.Errorf("getting completion: %w", err)
	}

	return check, nil
}

func (s *ModeratingSrv) getNewScore(score int, delta int) int {
	newScore := score + delta

	if newScore <= s.BanScore {
		return s.BanScore
	}

	if newScore >= s.TrustedScore {
		return s.TrustedScore
	}

	return newScore
}

type ScoreStore interface {
	GetScore(ctx context.Context, sender e.User, defaultValue int) (int, error)
	SetScore(ctx context.Context, sender e.User, score int) error
}

type MessagesStore interface {
	SaveMessage(ctx context.Context, msg e.Message) (int64, error)
	SaveAction(ctx context.Context, messageID int64, action e.Action) error
	SaveError(ctx context.Context, messageID int64, error string) error
}

type AIClient interface {
	GetJSONCompletion(ctx context.Context, system, user string, rf ai.ResponseFormat, result any) (*ai.Usage, error)
}

var noop = e.Action{
	Kind: e.ActionKindNoop,
	Note: "",
}

//go:embed system_prompt.txt
var prompt string
