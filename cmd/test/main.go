package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

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

	messages, err := db.ListMessages(ctx, time.Now().Add(time.Hour*24*30*-1))
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
			checkBatch(ctx, log, llm, batch)
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

func checkBatch(ctx context.Context, log logger.Logger, llm *ai.OpenAI, batch []e.SavedMessage) {
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

		var checkResult ai.SpamCheck
		_, err := llm.GetJSONCompletion(ctx, prompt, msg.Text, ai.SpamCheckFormat, &checkResult)
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
