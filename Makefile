DOCKER_IMAGE=registry.nuclight.org/antispam-tg-bot:latest

.PHONY: lint
lint:
	go tool golangci-lint run

.PHONY: run
run:
	go run cmd/bot/main.go

.PHONY: docker_build
docker_build:
	docker build \
		--platform linux/amd64 \
		--tag $(DOCKER_IMAGE) \
		.

.PHONY: docker_publish
docker_publish: docker_build
	docker push $(DOCKER_IMAGE)

.PHONY: pull_db
pull_db:
	scp nuclight.org:antispam-tg-bot/db/antispam.sqlite ./db/antispam.sqlite