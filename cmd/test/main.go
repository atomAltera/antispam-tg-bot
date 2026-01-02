package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	_ "embed"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/jessevdk/go-flags"
	"nuclight.org/antispam-tg-bot/app/storage"
	"nuclight.org/antispam-tg-bot/pkg/ai"
	e "nuclight.org/antispam-tg-bot/pkg/entities"
	"nuclight.org/antispam-tg-bot/pkg/logger"
)

var opts struct {
	DBPath      string `long:"db-path" env:"DB_PATH" required:"true" description:"path to the sqlite database file"`
	OpenAIKey   string `long:"ai-key" env:"OPENAI_KEY" required:"true" description:"ai api key"`
	TelegramKey string `long:"tg-key" env:"TELEGRAM_KEY" description:"telegram bot api key (optional, for image analysis)"`
}

//go:embed system_prompt.txt
var prompt string

var wg sync.WaitGroup
var processed int64
var becomeSpam int64
var becomeNotSpam int64
var stayTheSame int64

func main() {
	_, err := flags.Parse(&opts)
	if err != nil {
		os.Exit(1)
	}

	log := logger.NewLogger()
	log.Info("starting test")

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	db, err := storage.NewSQLite(ctx, opts.DBPath)
	if err != nil {
		log.Error("creating sqlite3 database", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Error("closing sqlite3 database", "error", err)
		}
	}()

	llm := ai.NewOpenAI(opts.OpenAIKey, http.DefaultClient)

	var downloader *mediaDownloader
	if opts.TelegramKey != "" {
		downloader, err = newMediaDownloader(opts.TelegramKey)
		if err != nil {
			log.Error("creating media downloader", "error", err)
			os.Exit(1)
		}
		log.Info("telegram media downloader enabled")
	}

	messages, err := db.ListMessages(ctx, time.Now().Add(time.Hour*24*10*-1))
	if err != nil {
		log.Error("listing messages from database", "error", err)
		os.Exit(1)
	}

	log.Info("messages loaded from database", "count", len(messages))

	dedup := make(map[string]struct{}, len(messages))
	unique := make([]e.SavedMessage, 0, len(messages))

	for _, msg := range messages {
		key := normalize(msg.Text)
		if _, exists := dedup[key]; exists {
			//log.Warn("duplicate message found", "text", msg.Text, "id", msg.ID)
			continue
		}
		dedup[key] = struct{}{}

		unique = append(unique, msg)
	}

	const workers = 10

	batchSize := (len(unique) + workers - 1) / workers

	for i := 0; i < workers; i++ {
		start := i * batchSize
		end := start + batchSize
		if end > len(unique) {
			end = len(unique)
		}
		if start >= end {
			break
		}

		wg.Add(1)
		go func(batch []e.SavedMessage) {
			defer wg.Done()
			checkBatch(ctx, log, llm, downloader, batch)
		}(unique[start:end])
	}

	wg.Wait()

	log.Info("done",
		"processed", processed,
		"stay_the_same", stayTheSame,
		"become_spam", becomeSpam,
		"become_not_spam", becomeNotSpam,
	)

	os.Exit(0)
}

func checkBatch(ctx context.Context, log logger.Logger, llm *ai.OpenAI, downloader *mediaDownloader, batch []e.SavedMessage) {
	for _, msg := range batch {
		if n := atomic.AddInt64(&processed, 1) + 1; n%10 == 0 {
			log.Debug("processing message", "n", n)
		}

		var wasSpam bool
		if msg.Action == nil {
			log.Debug("message without action", "id", msg.ID, "text", msg.Text)
			continue
		}
		if a := *msg.Action; a == e.ActionKindBan || a == e.ActionKindErase {
			wasSpam = true
		}

		text := msg.Text
		if text == "" {
			text = "(no text, analyze image only)"
		}

		var checkResult ai.SpamCheck
		var err error

		// Try to use image analysis if media is available and supported
		var mediaContent []byte
		var mediaType string
		if msg.MediaType != nil && ai.IsVisionSupported(*msg.MediaType) {
			mediaType = *msg.MediaType
			if downloader != nil && msg.MediaFileID != nil {
				mediaContent, err = downloader.DownloadFile(ctx, *msg.MediaFileID)
				if err != nil {
					log.Warn("downloading media from telegram", "error", err, "file_id", *msg.MediaFileID)
					mediaContent = nil
				}
			}
		}

		if len(mediaContent) > 0 {
			_, err = llm.GetJSONCompletionWithImage(ctx, prompt, text, mediaContent, mediaType, ai.SpamCheckFormat, &checkResult)
		} else {
			_, err = llm.GetJSONCompletion(ctx, prompt, text, ai.SpamCheckFormat, &checkResult)
		}

		if err != nil {
			if errors.Is(err, context.Canceled) {
				log.Info("context canceled, stopping")
				return
			}

			log.Error("getting completion", "error", err, "text", msg.Text)
			continue
		}

		if checkResult.IsSpam == wasSpam {
			atomic.AddInt64(&stayTheSame, 1)
			//log.Info("message is consistent with previous action", "text", msg.Text)
			continue
		}

		if !wasSpam && checkResult.IsSpam {
			atomic.AddInt64(&becomeSpam, 1)
			log.Info("became spam", "text", msg.Text, "note", checkResult.Note, "user", msg.Sender.Name, "time", msg.CreatedAt)
			continue
		}

		if wasSpam && !checkResult.IsSpam {
			atomic.AddInt64(&becomeNotSpam, 1)
			log.Warn("became not a spam", "text", msg.Text, "user", msg.Sender.Name, "time", msg.CreatedAt)
			continue
		}

		select {
		case <-ctx.Done():
			log.Info("context done, stopping")
			return
		default:
		}
	}

}

func normalize(text string) string {
	return strings.TrimSpace(strings.ToLower(text))
}

// mediaDownloader downloads media files from Telegram by file ID
type mediaDownloader struct {
	bot *tgbotapi.BotAPI
}

func newMediaDownloader(token string) (*mediaDownloader, error) {
	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("creating bot api: %w", err)
	}
	return &mediaDownloader{bot: bot}, nil
}

func (d *mediaDownloader) DownloadFile(ctx context.Context, fileID string) ([]byte, error) {
	file, err := d.bot.GetFile(tgbotapi.FileConfig{FileID: fileID})
	if err != nil {
		return nil, fmt.Errorf("getting file: %w", err)
	}

	fileURL := file.Link(d.bot.Token)
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
