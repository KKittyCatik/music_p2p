.PHONY: build run test lint docker-build up down logs swagger

build:
	go build -o bin/music_p2p ./cmd/node

run: build
	./bin/music_p2p --api-port 8080 --metrics-port 9090

test:
	go test -race -count=1 ./...

lint:
	go vet ./...

docker-build:
	docker build -t music_p2p:latest .

up:
	docker compose up -d --build

down:
	docker compose down -v

logs:
	docker compose logs -f

swagger:
	swag init -g internal/api/server.go -o docs/
