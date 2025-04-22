package moderator

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"

	e "nuclight.org/antispam-tg-bot/pkg/entities"
	"nuclight.org/antispam-tg-bot/pkg/logger"
)

var noop = e.Action{
	Kind: e.ActionKindNoop,
	Note: "",
}

// Handler is a handler of new messages. It decides what to do with a message
// based on the score system for each user. New user receives a default score.
// If score is lower than trusted score, the message is checked for spam. If the
// message is spam, the user receives a penalty score -1 and erase message action is
// returned. If the score reaches ban score, ban action is returned which also erases
// the message. If spam check returns false, score is increased by 1, and noop action
// is returned. When user reaches trusted score, the message is not checked for spam
// anymore.
type Handler struct {
	// Log is a logger
	Log logger.Logger

	// DefaultScore is a default score for a new user
	DefaultScore int

	// TrustedScore is a score for a trusted user
	TrustedScore int

	// BanScore is a score for a banned user
	BanScore int
}

// HandleMessage handles a message, it takes a message, reviews it and returns an action to be taken
// based on the score system. It returns an action and an error if something goes wrong. Returned
// action has to be considered even if error is not nil.
func (h *Handler) HandleMessage(ctx context.Context, msg e.Message) (e.Action, error) {
	userKey := buildUserKeyFromMessage(msg.Source, msg.ChatID, msg.SenderUserID)

	score, err := h.getScore(ctx, userKey, h.DefaultScore)
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

		err = h.setScore(ctx, userKey, newScore)
		if err != nil {
			return action, fmt.Errorf("setting user score: %w", err)
		}

		return action, nil
	}

	newScore := score + 1
	err = h.setScore(ctx, userKey, newScore)
	if err != nil {
		return noop, fmt.Errorf("setting user score: %w", err)
	}

	return noop, nil
}

func buildUserKeyFromMessage(src e.Source, chatID, userID string) string {
	hash := sha1.New()
	hash.Write([]byte(src))
	hash.Write([]byte(chatID))
	hash.Write([]byte(userID))

	digest := hash.Sum(nil)

	return hex.EncodeToString(digest[:16])
}
