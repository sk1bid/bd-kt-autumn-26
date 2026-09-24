.PHONY: test up migrate seed demo down catalog build
up:
	docker compose --profile test up -d --wait
migrate: up
	go run ./cmd/kt02 migrate
seed: migrate
	go run ./cmd/kt02 seed
demo: seed
	go run ./cmd/kt02 demo
	docker compose --profile test exec -T db psql -U kt02 -d transport < sql/report.sql
test: up
	go test ./...
	go run ./cmd/kt02 test
catalog: migrate
	go run ./cmd/kt02 dictionary
build:
	go build -o bin/kt02 ./cmd/kt02
down:
	docker compose --profile test down
