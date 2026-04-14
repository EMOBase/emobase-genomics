# ARG declared before all FROMs so it is available in every stage's FROM line.
ARG BLAST_VERSION=2.17.0

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

# ─── Stage 2: BLAST+ binaries ────────────────────────────────────────────────
FROM docker.io/ncbi/blast-static:${BLAST_VERSION} AS ncbi-blast

# ─── Stage 3: Runtime ────────────────────────────────────────────────────────
FROM debian:bookworm-slim

# Install CA certificates and timezone data (needed for HTTPS and time parsing)
RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates \
      tzdata \
      libsqlite3-0 \
      libgomp1 \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy the compiled binary from the builder
COPY --from=builder /app/server .

# Copy BLAST+ binaries and expose them on PATH
COPY --from=ncbi-blast \
  /blast/bin/blast_formatter \
  /blast/bin/blastdbcmd \
  /blast/bin/blastn \
  /blast/bin/blastp \
  /blast/bin/blastx \
  /blast/bin/makeblastdb \
  /blast/bin/tblastn \
  /blast/bin/tblastx \
  /blast/bin/
ENV PATH=/blast/bin:${PATH}

COPY internal/pkg/config/config.yaml /app/config.yaml
COPY migrations ./migrations

# Create the uploads directory so the volume mount point exists with correct perms
RUN mkdir -p ./public/uploads

ENTRYPOINT ["./server"]
