# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

EMOBase Genomics is a Go backend that ingests, indexes, and serves genomic data (genome
annotations, sequences, orthology, synonyms, dsRNA silencing sequences) for a single
configured "main species" plus related species, backed by MySQL (structured/relational
data: versions, jobs, upload files, app settings) and Elasticsearch (search/indexed
biological data). It also drives a JBrowse2 genome browser instance and BLAST databases
(via SequenceServer) as side effects of file uploads.

## Commands

There is no Makefile; everything runs through `go build`/`go run` or Docker Compose.
There are currently no `_test.go` files in the repository.

```bash
# Build the single binary (all subcommands are wired into one CLI, see cmd/main.go)
go build -o server ./cmd

# Run a subcommand directly
go run ./cmd api -c internal/pkg/config/config.yaml
go run ./cmd worker -c internal/pkg/config/config.yaml
go run ./cmd db:migrate -c internal/pkg/config/config.yaml --direction up
go run ./cmd es:migrate -c internal/pkg/config/config.yaml

# Vet / format
go vet ./...
gofmt -l .
```

Full local stack (recommended way to actually run/test the app end-to-end):

```bash
cp .env.example .env   # set KEYCLOAK__DEV_BYPASS_AUTH=true for local dev (no Keycloak service in compose)

docker compose --profile migrate run --rm --build db-migrate && \
docker compose --profile migrate run --rm --build es-migrate && \
docker compose --profile migrate run --rm --build setup-jbrowse2-web && \
docker compose up --build -d
```

Services: `api`, two `worker-N` instances, `mysql`, `elasticsearch`, `nginx` (public
entrypoint on :8000), `blast` (SequenceServer, BLAST database UI). `api` and `worker`
share bind-mounted volumes for uploads (`./data/public/uploads`) and the JBrowse2 web
root (`./data/jbrowse2`); workers additionally mount the Docker socket to restart the
`blast` container after rebuilding BLAST databases.

Config is YAML (`internal/pkg/config/config.yaml`) with env var overrides via Viper:
`SECTION__FIELD` (double underscore), prefixed with `EMOBASE_GENOMICS` by default
(override via `EMOBASE_GENOMICS_ENV_PREFIX`). See `.env.example` for the full set.

## Architecture

### CLI entrypoint

`cmd/main.go` defines one `urfave/cli` binary with four subcommands, each in its own
package: `cmd/api`, `cmd/worker`, `cmd/dbmigrate`, `cmd/esmigrate`. Each subcommand's
`Action` loads config, opens MySQL + ES connections, wires repositories → use cases →
handlers/router or job handlers, and runs. There is no DI framework — wiring is manual
and explicit in each `Action` function.

### Layering

`internal/pkg/` follows repository → usecase → handler:

- `repository/*` — one package per aggregate (`version`, `job`, `uploadfile`,
  `appsettings`, `genomic`, `sequence`, `orthology`, `synonym`, `dsrna`, `esindex`).
  MySQL-backed repos hold structured metadata; the ES-backed repos
  (genomic/sequence/orthology/synonym/dsrna) hold the actual searchable biological data,
  one index per `{prefix}-{type}-{versionName}-...`. `esindex` is special: it only
  manages cross-cutting index lifecycle (delete-by-version), not documents.
- `usecase/*` — business logic per domain, each usually takes its repo(s) as
  interfaces defined in a local `iface.go` in the same package (constructor-injected,
  no interface satisfaction checked at the repo layer). `usecase/versionresolver`
  centralizes "resolve a version name, or fall back to the app's configured default
  version" — used anywhere an empty/omitted version should mean "the default".
  `usecase/search` aggregates across the ES-backed repos for the public search/suggest/
  orthology/genes/silencingseqs endpoints.
- `api/handler/*` — thin Gin handlers calling into use cases; `api/router.go` wires
  routes and marks the admin-only route group behind `middleware.RequireAdmin`.
  Public (unauthenticated) routes: `/health`, `/docs`, `/docs/openapi.yaml`, `/search`,
  `/search/_suggest`, `/orthology/:species`, `/genes/:species`, `/silencingseqs`,
  `/public/versions`. Everything else (`/uploads`, `/versions`, `/jobs`,
  `/upload-files`) requires Keycloak auth (or dev bypass).

### Versions

A `Version` (MySQL, `entity.Version`) is a named snapshot of the whole dataset (e.g. a
release). Nearly every ES index and upload is scoped to a version by name. One version
can be marked the "default" via `appsettings` (`GetDefaultVersionID`) — the public API
and `versionresolver` fall back to it when no version name is given. Deleting a version
must clean up both MySQL rows and its ES indices (`esindex.DeleteIndexesByVersion`).

### Upload → job pipeline (the core async workflow)

Uploads use `tus` (resumable uploads) via `usecase/upload`, mounted at `/uploads`. Flow:

1. `PreUploadCreateCallback` (`handlePreUploadCreate`) validates `fileType`,
   `fileName`, per-type required metadata (e.g. `geneIDKey`/`trimPrefixChars` for
   `genomic.gff`, `order`/`algorithm` for `orthology.tsv`), rejects non-gzip files by
   extension, resolves the target `Version`, and rejects if an active job of the same
   file type already exists for that version (except `jbrowse.track`, which allows
   concurrent tracks).
2. On upload completion (`PreFinishResponseCallback` / `handlePreFinish`), the file is
   gzip-magic-byte verified, moved from the tus staging dir into
   `{uploadDir}/{version}/{fileName}`, and one or more `entity.Job` rows are enqueued
   in MySQL (status `PENDING`) via `enqueueProcessJob`. Job payloads are typed structs
   in `internal/pkg/jobpayload/*`, JSON-marshaled into `Job.Payload`.
3. `cmd/worker` polls MySQL for pending jobs (`ClaimNextPending`), dispatches to a
   `map[jobType]Handler` in `internal/pkg/usecase/worker/handlers/`, marks the job
   `DONE`/`FAILED`, and calls optional `OnCompleteHook`/`OnFailureHook` on the handler.
   A separate ticker (`runStuckJobRecovery`) requeues jobs stuck in `RUNNING` past a
   configured timeout back to `PENDING`.
4. Some uploads implicitly enqueue **multiple** jobs / chain later jobs based on other
   jobs' state — this cross-job logic lives in `usecase/upload/upload.go`, not in the
   worker: e.g. uploading `genomic.gff` also enqueues a `SPECIES.SYNONYM` job for the
   main species; `GENOMIC.GFF:SETUP_JBROWSE2` is only enqueued once the corresponding
   `GENOMIC.FNA:SETUP_JBROWSE2` job is confirmed `DONE` (checked via
   `tryEnqueueGFFSetupJBrowse2`), since the FNA (assembly) track must exist in JBrowse2
   before the GFF (annotation) track can be added.
5. Job types are all constants in `entity/job.go` (`JobType*`), each with a
   human-readable entry in `JobDescriptions`. `:DELETE` and `:SETUP_BLAST` /
   `:SETUP_JBROWSE2` suffixes denote job variants derived from a base file type, not
   separate upload types.

`ReleaseVersion` (`usecase/version/version.go`) is the other job-enqueuing entrypoint
(besides upload completion): it builds BLAST databases for `genomic.fna`/`protein.faa`/
`rna.fna` via `*_SETUP_BLAST` jobs. BLAST DB paths are **global, not per-version**
(`{blast.db_path}/{genome,protein,rna}`), so a version that omits an optional file type
(`protein.faa`/`rna.fna`) enqueues a `*_REMOVE_BLAST` job instead of silently leaving a
previous version's database in place — see `handlers/blast_shared.go` for the shared
`finalizeBlastRelease` logic (used by both `SetupBlastHandler` and `RemoveBlastHandler`)
that waits for every setup/remove job of a version to finish, then promotes it to
default, restarts the `blast` container, and promotes its JBrowse2 assembly to the
default view (see below). Both job payloads carry `VersionName` (not just the numeric
version ID) since the JBrowse2 promotion step needs it.

When adding a new upload-driven file type: add validation in `handlePreUploadCreate`,
a payload struct in `jobpayload/`, a job-creation branch in `enqueueProcessJob`, a
handler in `usecase/worker/handlers/` registered in `cmd/worker/worker.go`'s
`jobHandlers` map, and (if deletable) an entry in `upload.deletableFileTypes`.

### JBrowse2 and BLAST integration

Workers shell out to the `jbrowse` CLI and scripts in `scripts/` (`setup_jbrowse2_fna.sh`,
`setup_jbrowse2_gff.sh`, `add_jbrowse_track.sh`, `delete_jbrowse_version.sh`,
`set_default_jbrowse2_view.sh`, `setup_blast.sh`, `remove_blast.sh`) to mutate the
JBrowse2 web root (`/web`, bind-mounted from `./data/jbrowse2`) and BLAST databases
(`/db`, via `makeblastdb`, then restarting the `blast` container over the Docker
socket). `config.json` under the JBrowse2 web root is mutated concurrently by
multiple workers/scripts — **all writers must flock `/web/data/.jbrowse-config.lock`**
before touching it (a previously-fixed data-loss bug; see any script that writes
`config.json` for the pattern).

JBrowse2 assembly/track creation (`*_SETUP_JBROWSE2` jobs) happens at **upload**
completion, independent of whether/when a version is released — every version that
ever uploads `genomic.fna` gets an assembly and a `defaultSession.views` entry,
regardless of release status. Nothing about that entry reflects "this is the
currently active version" by itself, and old entries are never removed just because
a newer version is released (only explicit version deletion, via
`delete_jbrowse_version.sh`, removes one). To keep the app's default assembly
findable, `finalizeBlastRelease` (see above) calls `set_default_jbrowse2_view.sh`
once a version is promoted to default: it moves that version's existing view to the
front of `defaultSession.views` (so JBrowse2 opens it first with no explicit
session — the raw JBrowse2 app served at `/jbrowse2/` via nginx has no query params
to select a view otherwise) and prunes any views whose assembly no longer exists in
`.assemblies` (stale views left by an old version delete). It does not delete
history — older versions' assemblies/tracks remain and are still manually
selectable via JBrowse2's own UI.

### Elasticsearch conventions

- Index templates (mappings/analyzers) are created once via `es:migrate`
  (`cmd/esmigrate/esmigrate.go`), applied as composable index templates matching
  `{prefix}-{type}-*` so any dynamically created index inherits them.
- Actual indices are per-version, created at ingest time by the relevant repo.
- ES 8 rejects wildcard patterns in `Indices.Delete` by default
  (`action.destructive_requires_name`). Always resolve wildcards with `Indices.Get`
  first, then delete by explicit resolved names — see `esindex.DeleteIndexesByVersion`
  for the reference pattern. Never call `Indices.Delete` with a wildcard string
  directly.

### Auth

`internal/pkg/auth` validates Keycloak-issued JWTs (via `lestrrat-go/jwx`) and enforces
a required realm role. `KEYCLOAK__DEV_BYPASS_AUTH=true` skips signature verification
entirely for local development — never rely on it being enabled in any deployed
environment.

### API docs

`docs/openapi.yaml` is the OpenAPI spec, embedded via `docs/embed.go` and served at
`/docs` (Swagger UI) and `/docs/openapi.yaml`. Update this spec when adding/changing
routes in `api/router.go`.
