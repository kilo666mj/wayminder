.PHONY: build test test-race fmt vet tidy up down logs config

build:
	go build ./cmd/wayminder

test:
	go test ./...

test-race:
	go test -race ./...

fmt:
	gofmt -w cmd internal

vet:
	go vet ./...

tidy:
	go mod tidy

config:
	docker compose config --quiet

up:
	docker compose up -d --build

down:
	docker compose down

logs:
	docker compose logs -f wayminder
