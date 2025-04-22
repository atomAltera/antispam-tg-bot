package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/jessevdk/go-flags"
	"nuclight.org/antispam-tg-bot/app/moderator"
	"nuclight.org/antispam-tg-bot/app/storage"
	"nuclight.org/antispam-tg-bot/app/telegram"
	"nuclight.org/antispam-tg-bot/pkg/logger"
)

var opts struct {
	TelegramAPIToken   string `long:"telegram-api-token" env:"TELEGRAM_API_TOKEN" required:"true" description:"telegram api token"`
	TelegramWorkersNum int    `long:"telegram-workers-num" env:"TELEGRAM_WORKERS_NUM" default:"5" description:"number of workers for telegram bot"`
	DBPath             string `long:"db-path" env:"DB_PATH" default:"./db/antispam.sqlite" description:"path to the sqlite database file"`
}

var Revision = "dev"

func main() {
	_, err := flags.Parse(&opts)
	if err != nil {
		os.Exit(1)
	}

	log := logger.NewLogger()
	log.Info("starting bot", "revision", Revision)

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

	mod := &moderator.Handler{
		Log:           log,
		DefaultScore:  -3,
		TrustedScore:  0,
		BanScore:      -4,
		ScoreStore:    db,
		MessagesStore: db,
	}

	bot := &telegram.Client{
		Log:        log,
		APIToken:   opts.TelegramAPIToken,
		WorkersNum: opts.TelegramWorkersNum,
		Handler:    mod,
	}

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
