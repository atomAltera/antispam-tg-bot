package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/jessevdk/go-flags"
	"nuclight.org/antispam-tg-bot/app/services"
	"nuclight.org/antispam-tg-bot/app/storage"
	"nuclight.org/antispam-tg-bot/app/telegram"
	"nuclight.org/antispam-tg-bot/pkg/ai"
	"nuclight.org/antispam-tg-bot/pkg/logger"
)

var opts struct {
	TelegramAPIToken   string `long:"telegram-api-token" env:"TELEGRAM_API_TOKEN" required:"true" description:"telegram api token"`
	TelegramWorkersNum int    `long:"telegram-workers-num" env:"TELEGRAM_WORKERS_NUM" default:"5" description:"number of workers for telegram bot"`
	DBPath             string `long:"db-path" env:"DB_PATH" required:"true" description:"path to the sqlite database file"`
	OpenAIKey          string `long:"ai-key" env:"OPENAI_KEY" required:"true" description:"ai api key"`
	DevMode            bool   `long:"dev-mode" env:"DEV_MODE" description:"enable dev mode"`
}

func main() {
	_, err := flags.Parse(&opts)
	if err != nil {
		os.Exit(1)
	}

	log := logger.NewLogger()
	log.Info("starting bot", "dev_mode", opts.DevMode)

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

	openAIClient := ai.NewOpenAI(opts.OpenAIKey, http.DefaultClient)

	moderatingSrv := &services.ModeratingSrv{
		DefaultScore:  0,
		TrustedScore:  10,
		BanScore:      -4,
		ScoreStore:    db,
		MessagesStore: db,
		AI:            openAIClient,
	}

	bot := &telegram.Client{
		Log:        log,
		APIToken:   opts.TelegramAPIToken,
		WorkersNum: opts.TelegramWorkersNum,
		DevMode:    opts.DevMode,
		Handler:    moderatingSrv,
	}
	moderatingSrv.MediaDownloader = bot

	err = bot.Start(ctx)
	if err != nil {
		log.Error("starting bot", "error", err)
		os.Exit(1)
	}

	<-ctx.Done()
	log.Info("stopping bot")

	bot.Wait()

	os.Exit(0)
}
