include internal/kt03/tasks.mk

KT04_COMPOSE = docker compose -f deploy/kt04/compose.yaml -p transport-kt04

.PHONY: kt04-proxy kt04-proxy-check kt05-sql kt05-mongo
kt04-proxy:
	$(KT04_COMPOSE) up -d haproxy proxy-client
kt04-proxy-check:
	deploy/kt04/check-proxy.sh
	go run ./cmd/kt04 analytics
kt05-sql:
	sql/kt05/run.sh
kt05-mongo:
	docker compose -f deploy/kt05/compose.yaml up -d --wait
	docker compose -f deploy/kt05/compose.yaml exec -T mongo mongosh --quiet /examples/crud.js
