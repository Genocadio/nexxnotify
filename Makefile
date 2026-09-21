.PHONY: run build vet test fmt tidy sqlc db-up db-down up down

up:
	docker compose up --build -d

down:
	docker compose down

run:
	go run ./cmd/api

build:
	go build -o bin/api ./cmd/api

vet:
	go vet ./...

test:
	go test ./...

fmt:
	gofmt -l -w .

tidy:
	go mod tidy

sqlc:
	docker run --rm -v "$$PWD:/src" -w /src sqlc/sqlc:latest generate

db-up:
	docker compose up -d postgres

db-down:
	docker compose down
