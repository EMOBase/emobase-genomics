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

# Install system dependencies:
#   - ca-certificates, tzdata  : HTTPS and time parsing
#   - libsqlite3-0, libgomp1   : BLAST+ runtime libraries
#   - samtools, tabix          : genome file processing (tabix also provides bgzip)
#   - nodejs                   : required by JBrowse CLI
#   - wget                     : download JBrowse CLI
#   - gzip                     : file compression/decompression
#   - bash                     : interactive shell for local development
RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates \
      tzdata \
      libsqlite3-0 \
      libgomp1 \
      samtools \
      tabix \
      nodejs \
      wget \
      gzip \
      bash \
    && rm -rf /var/lib/apt/lists/*

# Install JBrowse CLI (no Node.js required — unpkg serves the standalone bundle)
RUN wget -q https://unpkg.com/@jbrowse/cli/bundle/index.js -O /usr/local/bin/jbrowse \
    && chmod +x /usr/local/bin/jbrowse

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
COPY scripts ./scripts
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

# Create the uploads directory so the volume mount point exists with correct perms
RUN mkdir -p ./public/uploads

# Dedicated temp directory for JBrowse2 setup — inside the image, never mounted
# in nginx, so users cannot access intermediate decompressed files.
RUN mkdir -p /jbrowse2-tmp

ENTRYPOINT ["/entrypoint.sh", "./server"]
