# EMOBase Genomics

## Requirements

- Docker

## How to Run

1. Start the application:

  ```bash
  docker compose --profile migrate run --rm db-migrate && \
  docker compose --profile migrate run --rm es-migrate && \
  docker compose --profile migrate run --rm setup-jbrowse2-web && \
  docker compose up -d
  ```

2. Configuration can be overridden using environment variables.
For example, to override `http.mode`, set the environment variable `HTTP__MODE=`.
