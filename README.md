# Telegram Anti-Spam Bot

A Telegram bot that uses AI to detect and remove spam messages, with a focus on filtering out job-related spam messages in group chats.

## Features

- AI-powered message moderation using OpenAI's GPT models
- User reputation system that rewards good behavior and penalizes spam
- Automatic message deletion for detected spam
- User banning for repeat offenders
- SQLite storage for persistent data
- Configurable scoring thresholds
- Detailed spam reports with reasons for moderation actions
- Enhanced message handling through optimized Telegram update processing

## How It Works

The bot uses a reputation scoring system:
- New users start with a default score
- Messages from users below the trusted threshold are checked by AI
- Non-spam messages earn users +1 point
- Spam messages result in -1 point, message deletion, and a detailed report
- Once users reach the trusted score, their messages bypass checks

## Requirements

- Go 1.20+
- OpenAI API key
- Telegram Bot API token

## Configuration

The application is configured via environment variables or command line flags:

| Parameter | Flag | Environment Variable | Description |
|-----------|------|----------------------|-------------|
| Telegram API Token | `--telegram-api-token` | `TELEGRAM_API_TOKEN` | Your Telegram Bot API token (required) |
| Workers | `--telegram-workers-num` | `TELEGRAM_WORKERS_NUM` | Number of Telegram workers (default: 5) |
| Database Path | `--db-path` | `DB_PATH` | Path to SQLite database (default: ./db/antispam.sqlite) |
| OpenAI API Key | `--ai-key` | `OPENAI_KEY` | Your OpenAI API key (required) |

## Installation

1. Clone the repository
2. Create a database directory: `mkdir -p db`
3. Set up required environment variables or prepare to use command-line flags

## Usage

Build and run with Make:

```bash
make run
```

Or directly with Go:

```bash
go run cmd/bot/main.go --telegram-api-token=YOUR_TOKEN --ai-key=YOUR_OPENAI_KEY
```

## Development

The project follows standard Go project layout:
- `cmd/` - Application entrypoints
- `app/` - Application-specific code
  - `services/` - Core business logic
  - `storage/` - Data persistence
  - `telegram/` - Telegram integration
- `pkg/` - Reusable packages
  - `ai/` - AI client integration
  - `entities/` - Domain entities
  - `logger/` - Logging utilities

## License

[MIT License](LICENSE)