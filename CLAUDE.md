# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Description

Telegram bot for automated spam detection and moderation. Uses AI-based message analysis with a user scoring system to identify and remove spam while building trust scores for legitimate users.

## Architecture

- `app/telegram/` - Telegram bot client with concurrent message processing and media download
- `app/services/` - Core business logic (moderator service, spam detection with multi-modal support)
  - `AIClient` interface: `GetJSONCompletion()` for text-only, `GetJSONCompletionWithImage()` for vision analysis
- `app/services/system_prompt.txt` - AI spam detection criteria (embedded in moderator)
- `app/storage/` - Data persistence layer (SQLite with schema migrations)
- `pkg/ai/` - AI client integration (OpenAI with text and image analysis)
  - `OpenAI` struct implements `AIClient` with vision support
  - Helper: `IsVisionSupported(mimeType)` validates image formats
- `pkg/entities/` - Domain entities (messages with media, actions, scores)
- `cmd/` - Application entry points

## Spam Detection Criteria

The bot identifies spam messages based on categories defined in `app/services/system_prompt.txt`:

- Job offers and vacancy postings
- Project recruitment messages
- Concert ticket sales
- Cryptocurrency and NFT trading offers
- Casino and gambling promotions
- Adult services
- Loan and financial service offers
- Bot/service offers (discount finders, partner search, nude content)
- Homoglyph spam (Latin/Cyrillic character substitution)
- Vague calls to action without context
- Mass tagging of multiple usernames
- Obfuscated username mentions

Note: The chat allows non-informative messages and Mafia game-related content.

## Build & Run Commands
- `make run` - Run the Telegram bot application
- `make lint` - Run golangci-lint for static code analysis
- `go test ./...` - Run all tests (add `-v` for verbose output)
- `go test ./path/to/package` - Run tests in a specific package

## Code Style Guidelines
- **Imports**: Group standard library, external, and internal imports
- **Formatting**: Follow standard Go formatting (`gofmt`)
- **Types**: Define clear interfaces, use meaningful type names
- **Naming**: Use CamelCase for exported names, descriptive variable names
- **Error Handling**: Wrap errors with context using `fmt.Errorf("message: %w", err)`
- **Logging**: Use structured logging with `slog` package
- **Dependencies**: Manage with go modules; update with `go get -u`
- **Project Structure**: Follow standard Go layout (cmd/, pkg/, app/)
- **AI Models**: Use `gpt-5-mini` for both text and vision analysis (constants: `DefaultModel`, `VisionModel`)
- **Vision API**: Supports JPEG, PNG, WebP, GIF formats - check with `ai.IsVisionSupported(mimeType)`

## Patterns

- **Concurrent message processing**: Telegram client spawns multiple worker goroutines (`WorkersNum`) that read from shared update channel with context-aware cancellation
- **Entity helpers**: Domain entities provide `Has*()` methods for feature detection (e.g., `HasText()`, `HasMedia()`)
- **Multi-modal spam detection**: Moderator checks both text and media content
  - Condition: `HasMedia() && !MediaTruncated && MediaContent != nil && ai.IsVisionSupported(MediaType)`
  - Uses `GetJSONCompletionWithImage()` with base64-encoded data URLs
  - Supported formats: JPEG, PNG, WebP, GIF (verified via `VisionSupportedMimeTypes` map)
  - Falls back to text-only analysis if media unsupported or unavailable
  - Text defaults to "(no text, analyze image only)" when empty
- **AI reasoning effort**: OpenAI requests include `reasoning_effort: "medium"` for text-only analysis - omitted for vision model requests
- **Multi-modal message structure**: Vision requests use `[]ContentPart` with separate text and image_url objects (detail: "low" to save tokens)
- **Database migrations**: Schema changes use column-based migration system in `sqlite.go` with `migrateAddColumn()`
- **Media handling**: Messages support attachments (photos, videos, animations, documents, stickers) with 1MB size limit - content >1MB stored as metadata only with `MediaTruncated` flag
- **Media download**: Bot downloads media via Telegram File API, extracts MIME types, and truncates content exceeding `maxMediaSize` (1MB)
- **Message extraction**: Helper functions (`takeText()`, `takeMessage()`, `getMediaInfo()`) normalize Telegram API structures into domain entities
- **Embedded resources**: Database schemas and system prompts embedded using `//go:embed` directive

## When Making Changes
- Keep code simple and maintainable
- Add comments for non-obvious logic
- Run linter before committing