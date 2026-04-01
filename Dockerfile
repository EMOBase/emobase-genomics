# ─── Stage 1: Builder ────────────────────────────────────────────────────────
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Install dependencies first (layer cache)
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build a static binary
COPY ./cmd ./cmd
COPY ./internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -o server ./cmd

# ─── Stage 2: Runtime ────────────────────────────────────────────────────────
FROM alpine:3

# Install CA certificates (needed for HTTPS outbound calls)
RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

# Copy the compiled binary from the builder
COPY --from=builder /app/server .
COPY internal/pkg/config/config.yaml /app/config.yaml

# Create the uploads directory so the volume mount point exists with correct perms
RUN mkdir -p ./public/uploads

ENTRYPOINT ["./server"]
