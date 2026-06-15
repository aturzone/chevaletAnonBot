# Multi-stage build for the Go port of ChevaletAnonBot.
# Build context must be the repository root (see deploy/go/docker-compose.yml).

FROM golang:1.25-alpine AS build
WORKDIR /src

# Cache module downloads.
COPY go.mod go.sum ./
RUN go mod download

# Build the static binary.
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/bot ./cmd/bot

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app

COPY --from=build /out/bot /app/bot
# Static message templates are read from ./Texts at runtime.
COPY Texts /app/Texts

ENTRYPOINT ["/app/bot"]
