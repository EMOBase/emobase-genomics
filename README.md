# EMOBase Genomics

## Requirements

- Docker

## How to Run

1. Start the application:

  ```bash
  docker compose --profile migrate up migrate es-migrate
  docker compose up -d
  ```

2. Configuration can be overridden using environment variables.
For example, to override `http.mode`, set the environment variable `HTTP__MODE=`.
