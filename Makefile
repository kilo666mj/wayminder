.PHONY: build test test-race fmt vet tidy up down logs config maintain probe

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

# Offline reports against the running stack; pass flags with ARGS="...".
maintain:
	docker compose run --rm --no-deps wayminder maintain $(ARGS)

probe:
	docker compose run --rm --no-deps wayminder probe $(ARGS)
