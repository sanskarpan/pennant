# syntax=docker/dockerfile:1

# ── Stage 1: Build frontend ──────────────────────────────────────────────────
FROM oven/bun:1 AS frontend-builder
WORKDIR /app/frontend
COPY frontend/package.json frontend/bun.lock* ./
RUN bun install --frozen-lockfile
COPY frontend/ ./
RUN bun run build

# ── Stage 2: Build Go binary ─────────────────────────────────────────────────
FROM golang:1.26-alpine AS go-builder
RUN apk add --no-cache gcc musl-dev
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Embed frontend dist into binary via go:embed (or copy to static/ dir)
RUN CGO_ENABLED=1 GOOS=linux go build -o pennant ./cmd/server/

# ── Stage 3: Final minimal image ─────────────────────────────────────────────
FROM alpine:3.19
RUN apk add --no-cache ca-certificates sqlite-libs
WORKDIR /app
COPY --from=go-builder /app/pennant .
COPY --from=frontend-builder /app/frontend/dist ./static/
EXPOSE 8080
ENTRYPOINT ["./pennant"]
