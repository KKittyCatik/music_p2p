# Stage 1: build the Go binary
FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git gcc musl-dev alsa-lib-dev

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -o /app/music_p2p ./cmd/node

# Stage 2: minimal runtime image
FROM alpine:3.19

RUN apk add --no-cache alsa-lib ca-certificates

COPY --from=builder /app/music_p2p /app/music_p2p

EXPOSE 4001 8080 9090

ENTRYPOINT ["/app/music_p2p"]
CMD ["--listen", "4001", "--api-port", "8080", "--metrics-port", "9090"]
