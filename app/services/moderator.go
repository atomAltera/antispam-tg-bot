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

	// MediaDownloader downloads media content by file ID (on-demand)
	MediaDownloader MediaDownloader

	// MediaConverter turns media the vision API can't decode directly
	// (e.g. video stickers) into a still image. Optional: if nil, such
	// media is treated as non-analyzable.
	MediaConverter MediaConverter
}

// HandleMessage handles a message, it takes a message, reviews it and returns an action to be taken
// based on the score system. It returns an action and an error if something goes wrong. Returned
// action has to be considered even if error is not nil.
func (s *ModeratingSrv) HandleMessage(ctx context.Context, msg e.Message) (e.Action, error) {
	hasText := msg.HasText()
	hasAnalyzableMedia := s.analyzableMedia(msg)

	if !hasText && !hasAnalyzableMedia {
		// Nothing to analyze: no text and no analyzable media (or unsupported media type)
		return noop, nil
	}

	score, err := s.ScoreStore.GetScore(ctx, msg.Sender, s.DefaultScore)
	if err != nil {
		return noop, fmt.Errorf("getting user score: %w", err)
	}

	if score >= s.TrustedScore {
		if score > s.TrustedScore {
			// Adjust score down to the trusted score
			err = s.ScoreStore.SetScore(ctx, msg.Sender, s.TrustedScore)
			if err != nil {
				return noop, fmt.Errorf("setting user score to trusted: %w", err)
			}
		}

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
	report, err := s.checkSpam(ctx, msg)
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

func (s *ModeratingSrv) checkSpam(ctx context.Context, msg e.Message) (ai.SpamCheck, error) {
	var check ai.SpamCheck
	var err error

	text := msg.Text
	if text == "" {
		text = "(no text, analyze image only)"
	}

	if s.analyzableMedia(msg) {
		// Download media content on-demand
		var mediaContent []byte
		mediaContent, err = s.MediaDownloader.DownloadFile(ctx, *msg.MediaFileID)
		if err != nil {
			return check, fmt.Errorf("downloading media: %w", err)
		}

		mimeType := *msg.MediaType
		if s.canConvertMedia(msg) {
			// Media the vision API can't decode directly (e.g. video
			// stickers): extract a still frame and analyze that as JPEG.
			var frame []byte
			frame, err = s.MediaConverter.ToImage(ctx, mediaContent)
			if err != nil {
				// Conversion failed (corrupt media or an unavailable/broken
				// ffmpeg). If the message has text, degrade to text-only
				// analysis rather than skipping the spam check entirely -
				// otherwise spam text could bypass moderation by attaching
				// an unconvertible file. If the message is media-only there
				// is nothing real to analyze: report the error so the
				// failure is visible instead of scoring a placeholder.
				if !msg.HasText() {
					return check, fmt.Errorf("converting media to image: %w", err)
				}
				mediaContent = nil // err is overwritten by the text-only completion below
			} else {
				mediaContent = frame
				mimeType = "image/jpeg"
			}
		}

		if mediaContent != nil {
			_, err = s.AI.GetJSONCompletionWithImage(ctx, prompt, text, mediaContent, mimeType, ai.SpamCheckFormat, &check)
		} else {
			_, err = s.AI.GetJSONCompletion(ctx, prompt, text, ai.SpamCheckFormat, &check)
		}
	} else {
		_, err = s.AI.GetJSONCompletion(ctx, prompt, text, ai.SpamCheckFormat, &check)
	}

	if err != nil {
		return check, fmt.Errorf("getting completion: %w", err)
	}

	return check, nil
}

// maxConvertibleMediaSize bounds media we're willing to download and run
// through the MediaConverter. Telegram video stickers are capped at 256 KB,
// so this comfortably covers them while refusing to fetch and ffmpeg large
// video/webm documents or videos that merely share the same mime type.
const maxConvertibleMediaSize = 512 * 1024

// analyzableMedia reports whether the message carries media the bot can send
// to the vision pipeline - either a format the vision API supports directly,
// or one the MediaConverter can turn into a still image.
func (s *ModeratingSrv) analyzableMedia(msg e.Message) bool {
	if !msg.HasMedia() || msg.MediaFileID == nil || msg.MediaType == nil {
		return false
	}
	return ai.IsVisionSupported(*msg.MediaType) || s.canConvertMedia(msg)
}

// canConvertMedia reports whether a MediaConverter is configured, able to
// convert the message's mime type, and the media is small enough to be worth
// downloading and converting. The size guard keeps a video-sticker fix from
// pulling and processing arbitrarily large video/webm uploads.
func (s *ModeratingSrv) canConvertMedia(msg e.Message) bool {
	if s.MediaConverter == nil || msg.MediaType == nil {
		return false
	}
	if !s.MediaConverter.CanConvert(*msg.MediaType) {
		return false
	}
	// Require a known positive size: Telegram may omit file_size, which
	// decodes as 0 and must not slip past the size guard.
	return msg.MediaSize != nil && *msg.MediaSize > 0 && *msg.MediaSize <= maxConvertibleMediaSize
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
	GetJSONCompletionWithImage(ctx context.Context, system, user string, image []byte, mimeType string, rf ai.ResponseFormat, result any) (*ai.Usage, error)
}

type MediaDownloader interface {
	DownloadFile(ctx context.Context, fileID string) ([]byte, error)
}

// MediaConverter turns media types the vision API can't decode directly into a
// still JPEG image (e.g. extracting the first frame of a video sticker).
type MediaConverter interface {
	// CanConvert reports whether the given mime type can be converted.
	CanConvert(mimeType string) bool
	// ToImage returns a still JPEG image derived from the media content.
	ToImage(ctx context.Context, content []byte) ([]byte, error)
}

var noop = e.Action{
	Kind: e.ActionKindNoop,
	Note: "",
}

//go:embed system_prompt.txt
var prompt string
