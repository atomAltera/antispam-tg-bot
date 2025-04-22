
.PHONY: lint
lint:
	go tool golangci-lint run


.PHONY: run
run:
	go run cmd/bot/main.go