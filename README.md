# EMOBase Genomics

## Requirements

- Docker

## How to Run

1. Start the application:

  ```bash
  docker compose up -d --build && \
  docker compose --profile migrate run --rm migrate && \
  docker compose --profile migrate run --rm es-migrate
  ```

2. Configuration can be overridden using environment variables.
For example, to override `http.mode`, set the environment variable `HTTP__MODE=`.
