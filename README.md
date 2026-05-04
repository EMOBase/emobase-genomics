# EMOBase Genomics

## Requirements

- Docker

## How to Run

1. Copy the example environment file and update it if needed:

```bash
cp .env.example .env
```

2. Start the application:

```bash
docker compose --profile migrate run --rm --build db-migrate && \
docker compose --profile migrate run --rm --build es-migrate && \
docker compose --profile migrate run --rm --build setup-jbrowse2-web && \
docker compose up --build -d
```
