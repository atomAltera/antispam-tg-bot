package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	_ "embed"

	"github.com/jessevdk/go-flags"
	"nuclight.org/antispam-tg-bot/app/storage"
	"nuclight.org/antispam-tg-bot/pkg/ai"
	e "nuclight.org/antispam-tg-bot/pkg/entities"
	"nuclight.org/antispam-tg-bot/pkg/logger"
)

var opts struct {
	DBPath    string `long:"db-path" env:"DB_PATH" required:"true" description:"path to the sqlite database file"`
	OpenAIKey string `long:"ai-key" env:"OPENAI_KEY" required:"true" description:"ai api key"`
	Count     int    `short:"c" long:"count" default:"500" description:"number of messages to load from database"`
}

//go:embed system_prompt.txt
var prompt string

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

	messages, err := db.ListMessages(ctx, opts.Count)
	if err != nil {
		log.Error("listing messages from database", "error", err)
		os.Exit(1)
	}

	log.Info("messages loaded from database", "count", len(messages))

	dedup := make(map[string]struct{}, len(messages))

	for i, msg := range messages {
		if n := i + 1; n%50 == 0 {
			log.Debug("processing message", "n", n, "of", len(messages))
		}

		key := normalize(msg.Text)
		if _, exists := dedup[key]; exists {
			//log.Warn("duplicate message found", "text", msg.Text, "id", msg.ID)
			continue
		}
		dedup[key] = struct{}{}

		var wasSpam bool
		if msg.Action == nil {
			log.Debug("message without action", "id", msg.ID, "text", msg.Text)
			continue
		}
		if a := *msg.Action; a == e.ActionKindBan || a == e.ActionKindErase {
			wasSpam = true
		}

		var checkResult ai.SpamCheck
		_, err = llm.GetJSONCompletion(ctx, prompt, msg.Text, ai.SpamCheckFormat, &checkResult)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				log.Info("context canceled, stopping")
				return
			}

			log.Error("getting completion", "error", err, "text", msg.Text)
			continue
		}

		if checkResult.IsSpam == wasSpam {
			//log.Info("message is consistent with previous action", "text", msg.Text)
			continue
		}

		if !wasSpam && checkResult.IsSpam {
			log.Info("became spam", "text", msg.Text, "note", checkResult.Note, "user", msg.Sender.Name, "time", msg.CreatedAt)
			continue
		}

		if wasSpam && !checkResult.IsSpam {
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

	os.Exit(0)
}

func normalize(text string) string {
	return strings.TrimSpace(strings.ToLower(text))
}
