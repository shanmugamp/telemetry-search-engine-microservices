.PHONY: test test-race build up down logs clean

## Run all tests WITHOUT race detector (works everywhere, no CGO needed)
test:
	cd auth-service   && go test ./... -v -count=1
	cd search-service && go test ./... -v -count=1
	cd ingest-service && go test ./... -v -count=1

## Run tests WITH race detector (requires CGO_ENABLED=1, Linux/Mac only)
## On Windows: use WSL2 or Docker (see below)
test-race:
	cd auth-service   && CGO_ENABLED=1 go test ./... -v -race -count=1
	cd search-service && CGO_ENABLED=1 go test ./... -v -race -count=1
	cd ingest-service && CGO_ENABLED=1 go test ./... -v -race -count=1

## Run race-detector tests inside Docker (works on ALL platforms including Windows)
test-race-docker:
	docker run --rm -v "$(PWD)/auth-service:/app"   -w /app golang:1.24 go test ./... -v -race -count=1
	docker run --rm -v "$(PWD)/search-service:/app" -w /app golang:1.24 go test ./... -v -race -count=1
	docker run --rm -v "$(PWD)/ingest-service:/app" -w /app golang:1.24 go test ./... -v -race -count=1

## Build all Docker images
build:
	docker-compose build

## Start all services
up:
	mkdir -p parquet-data
	docker-compose up --build -d

## Stop all services
down:
	docker-compose down

## Stream logs from all services
logs:
	docker-compose logs -f

## Tear down completely (removes volumes)
clean:
	docker-compose down -v
	docker system prune -f
