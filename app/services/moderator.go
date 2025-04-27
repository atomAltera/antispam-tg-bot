package services

import (
	"context"
	_ "embed"
	"fmt"

	"nuclight.org/antispam-tg-bot/pkg/ai"
	e "nuclight.org/antispam-tg-bot/pkg/entities"
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
	if score <= s.BanScore {
		return e.Action{
			Kind: e.ActionKindBan,
			Note: fmt.Sprintf("user score is %d, while ban score is %d", score, s.BanScore),
		}, -1, nil
	}

	isSpam, err := s.checkSpam(ctx, msg.Text)
	if err != nil {
		return noop, 0, fmt.Errorf("checking spam: %w", err)
	}

	if !isSpam {
		return noop, 1, nil
	}

	return e.Action{
		Kind: e.ActionKindErase,
		Note: "message is a spam",
	}, -1, nil
}

func (s *ModeratingSrv) checkSpam(ctx context.Context, text string) (bool, error) {
	var answer ai.YesNoAnswer
	_, err := s.AI.GetJSONCompletion(ctx, prompt, text, ai.YesNoFormat, &answer)
	if err != nil {
		return false, fmt.Errorf("getting completion: %w", err)
	}

	return answer.Yes, nil
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
