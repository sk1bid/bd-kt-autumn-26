.DEFAULT_GOAL := up
.PHONY: up down kt03-init kt03-reset kt03-dev kt03-load kt03-scenarios kt03-compare kt03-all kt03-package kt03-report
up:
	docker compose --profile test up -d --wait
down:
	docker compose --profile test down
kt03-init: up
	docker compose --profile test exec -T db psql -X -v ON_ERROR_STOP=1 -U kt02 -d postgres < sql/kt03/create_database.sql
	go run ./cmd/kt02 migrate --database transport_kt03
kt03-reset: up
	docker compose --profile test exec -T db dropdb -U kt02 --if-exists transport_kt03
	$(MAKE) kt03-init
kt03-dev: up
	go run ./cmd/kt03 generate --mode dev --data data/kt03-dev
	go run ./cmd/kt03 load --database transport_kt03_dev --data data/kt03-dev --out artifacts/kt03/dev
	go run ./cmd/kt03 queries --database transport_kt03_dev --data data/kt03-dev --out artifacts/kt03/dev
	go run ./cmd/kt03 bench --database transport_kt03_dev --data data/kt03-dev --out artifacts/kt03/dev
kt03-load: up
	go run ./cmd/kt03 generate --mode load --data data/kt03-load
	go run ./cmd/kt03 load --mode load --data data/kt03-load --out artifacts/kt03/load
	go run ./cmd/kt03 queries --mode load --data data/kt03-load --out artifacts/kt03/load
	go run ./cmd/kt03 bench --mode load --data data/kt03-load --out artifacts/kt03/load
kt03-scenarios: up
	go run ./cmd/kt03 scenarios
kt03-compare: up
	go run ./cmd/kt03 load-compare --data data/kt03-dev
kt03-all:
	go test ./...
	$(MAKE) up
	go run ./cmd/kt03 environment
	go run ./cmd/kt03 regression
	go run ./cmd/kt03 pilot
	$(MAKE) kt03-dev
	$(MAKE) kt03-scenarios
	$(MAKE) kt03-compare
	$(MAKE) kt03-load
	$(MAKE) kt03-report
kt03-package:
	go run ./cmd/kt03 package
kt03-report:
	go run ./cmd/kt03 report
