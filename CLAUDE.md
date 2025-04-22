# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

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

## When Making Changes
- Keep code simple and maintainable
- Add comments for non-obvious logic
- Run linter before committing