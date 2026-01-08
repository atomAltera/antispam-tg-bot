package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/jessevdk/go-flags"
	"nuclight.org/antispam-tg-bot/app/storage"
	"nuclight.org/antispam-tg-bot/pkg/logger"
)

var opts struct {
	DBPath      string `long:"db-path" env:"DB_PATH" required:"true" description:"path to the sqlite database file"`
	TelegramKey string `long:"tg-key" env:"TELEGRAM_API_TOKEN" required:"true" description:"telegram bot api key"`
	OutputDir   string `long:"output" env:"OUTPUT_DIR" default:"./files" description:"output directory for downloaded files"`
	DaysBack    int    `long:"days" env:"DAYS_BACK" default:"10" description:"number of days back to fetch messages"`
	Workers     int    `long:"workers" env:"TELEGRAM_WORKERS_NUM" default:"5" description:"number of concurrent download workers"`
}

var (
	wg         sync.WaitGroup
	downloaded int64
	skipped    int64
	failed     int64
)

func main() {
	_, err := flags.Parse(&opts)
	if err != nil {
		os.Exit(1)
	}

	log := logger.NewLogger()
	log.Info("starting download")

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Create output directory
	if err := os.MkdirAll(opts.OutputDir, 0755); err != nil {
		log.Error("creating output directory", "error", err)
		os.Exit(1)
	}

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

	downloader, err := newMediaDownloader(opts.TelegramKey)
	if err != nil {
		log.Error("creating media downloader", "error", err)
		os.Exit(1)
	}

	fromDate := time.Now().Add(time.Hour * 24 * time.Duration(opts.DaysBack) * -1)
	messages, err := db.ListMessages(ctx, fromDate)
	if err != nil {
		log.Error("listing messages from database", "error", err)
		os.Exit(1)
	}

	log.Info("messages loaded from database", "count", len(messages), "from", fromDate.Format(time.RFC3339))

	// Filter messages with media files
	type downloadTask struct {
		fileID   string
		mimeType string
	}

	var tasks []downloadTask
	seen := make(map[string]struct{})

	for _, msg := range messages {
		if msg.MediaFileID == nil || msg.MediaType == nil {
			continue
		}
		fileID := *msg.MediaFileID
		if _, exists := seen[fileID]; exists {
			continue
		}
		seen[fileID] = struct{}{}
		tasks = append(tasks, downloadTask{
			fileID:   fileID,
			mimeType: *msg.MediaType,
		})
	}

	log.Info("files to download", "count", len(tasks))

	if len(tasks) == 0 {
		log.Info("no files to download")
		os.Exit(0)
	}

	// Create work channel
	taskChan := make(chan downloadTask, len(tasks))
	for _, task := range tasks {
		taskChan <- task
	}
	close(taskChan)

	// Start workers
	for i := 0; i < opts.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range taskChan {
				select {
				case <-ctx.Done():
					return
				default:
				}

				filename := task.fileID + getExtension(task.mimeType)
				filepath := filepath.Join(opts.OutputDir, filename)

				// Skip if file already exists
				if _, err := os.Stat(filepath); err == nil {
					atomic.AddInt64(&skipped, 1)
					continue
				}

				content, err := downloader.DownloadFile(ctx, task.fileID)
				if err != nil {
					log.Error("downloading file", "error", err, "file_id", task.fileID)
					atomic.AddInt64(&failed, 1)
					continue
				}

				if err := os.WriteFile(filepath, content, 0644); err != nil {
					log.Error("writing file", "error", err, "path", filepath)
					atomic.AddInt64(&failed, 1)
					continue
				}

				n := atomic.AddInt64(&downloaded, 1)
				if n%10 == 0 {
					log.Debug("progress", "downloaded", n)
				}
			}
		}()
	}

	wg.Wait()

	log.Info("done",
		"downloaded", downloaded,
		"skipped", skipped,
		"failed", failed,
	)
}

func getExtension(mimeType string) string {
	switch mimeType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "video/mp4":
		return ".mp4"
	case "video/webm":
		return ".webm"
	case "audio/mpeg":
		return ".mp3"
	case "audio/ogg":
		return ".ogg"
	case "application/pdf":
		return ".pdf"
	default:
		return ""
	}
}

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
