# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

EMOBase Genomics is a Go backend that ingests, indexes, and serves genomic data (genome
annotations, sequences, orthology, synonyms, dsRNA silencing sequences) for multiple
species within a single release, backed by MySQL (structured/relational data: versions,
assembly versions, jobs, upload files, app settings) and Elasticsearch (search/indexed
biological data). It also drives a JBrowse2 genome browser instance and BLAST databases
(via SequenceServer) as side effects of file uploads. See
`docs/design/multi-species-assembly-versions.md` for the full design behind the
Database Version / Assembly Version model described below.

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

- `repository/*` — one package per aggregate (`version`, `assemblyversion`, `job`,
  `uploadfile`, `appsettings`, `genomic`, `sequence`, `orthology`, `synonym`, `dsrna`,
  `esindex`). MySQL-backed repos hold structured metadata; the ES-backed repos
  (genomic/sequence/orthology/synonym/dsrna) hold the actual searchable biological data.
  Four of the five (genomic/sequence/synonym/dsrna) key their concrete index names as
  `{prefix}-{type}-{versionSlug}-{species}-...`, with multiple species' indices sharing
  one `{prefix}-{type}-{versionSlug}` alias; `orthology` has no species segment (see
  Assembly Versions below). `esindex` is special: it only manages cross-cutting index
  lifecycle (delete-by-version or delete-by-assembly-version), not documents.
- `usecase/*` — business logic per domain, each usually takes its repo(s) as
  interfaces defined in a local `iface.go` in the same package (constructor-injected,
  no interface satisfaction checked at the repo layer). `usecase/versionresolver`
  centralizes "resolve a version name, or fall back to the app's configured default
  version" — used anywhere an empty/omitted version should mean "the default".
  `usecase/assemblyversion` is the per-species CRUD/status/delete counterpart to
  `usecase/version`, reusing several of its exported helpers (`ComputeVersionStatus`,
  `BuildAssemblyVersionDetail`). `usecase/search` aggregates across the ES-backed repos
  for the public search/suggest/orthology/genes/silencingseqs endpoints.
- `api/handler/*` — thin Gin handlers calling into use cases; `api/router.go` wires
  routes and marks the admin-only route group behind `middleware.RequireAdmin`.
  Public (unauthenticated) routes: `/health`, `/docs`, `/docs/openapi.yaml`, `/search`,
  `/search/_suggest`, `/orthology/:species`, `/genes/:species`, `/silencingseqs`,
  `/public/versions`. Everything else (`/uploads`, `/versions`, `/versions/:name/assemblies`,
  `/jobs`, `/upload-files`) requires Keycloak auth (or dev bypass).

### Versions and Assembly Versions

A `Version` (MySQL, `entity.Version`) is a named snapshot of the whole dataset (e.g. a
release) — a "Database Version" in the design doc's terms. Each Version holds one or
more `AssemblyVersion` rows (MySQL, `entity.AssemblyVersion`), one per species: `species`
is a short admin-provided code (e.g. `"Hsap"`, unique within the Version, used
everywhere species are addressed in the API and in upload metadata) and `name` is a
free-text human label (e.g. `"Human / GRCh38"`). Every Assembly Version is fully
symmetric — there is no "primary"/"default" one. `upload_files` and `jobs` both keep
their original `version_id` column (unchanged) and additionally carry a nullable
`assembly_version_id`: populated for every per-species file type, left `NULL` only for
`orthology.tsv`, which is shared across every species in a Version rather than owned by
one (its own uploads/jobs/ES index stay Version-scoped only, exactly as before this
model existed). `entity.AssemblyVersion.AssemblyID()` returns an opaque
`"v{versionID}a{assemblyID}"` key used wherever JBrowse2/BLAST need a stable,
shell/filename-safe identifier instead of free-text `name`/`species`.

One Version can be marked the "default" via `appsettings` (`GetDefaultVersionID`) — the
public API and `versionresolver` fall back to it when no version name is given; this is
unchanged by the Assembly Version model — once a Version is default, *every* one of its
assemblies becomes servable (see JBrowse2/BLAST below). Deleting a Version must clean up
its assembly rows, both the whole-Version and per-assembly-scoped MySQL rows, ES indices
(`esindex.DeleteIndexesByVersion` for the whole Version; `DeleteIndexesByAssemblyVersion`
for one assembly), and JBrowse2 data.

### Upload → job pipeline (the core async workflow)

Uploads use `tus` (resumable uploads) via `usecase/upload`, mounted at `/uploads`. Flow:

1. `PreUploadCreateCallback` (`handlePreUploadCreate`) validates `fileType`,
   `fileName`, per-type required metadata (e.g. `geneIDKey`/`trimPrefixChars` for
   `genomic.gff`, `order`/`algorithm` for `orthology.tsv`), rejects non-gzip files by
   extension, resolves the target `Version`, then — for every file type except
   `orthology.tsv`, which is shared across the whole Version — resolves a required
   `assembly` metadata field (the target Assembly Version's `species` code) to a
   concrete `AssemblyVersion` via `assemblyVersionRepo.FindBySpecies`. Rejects if an
   active job of the same file type already exists for that assembly (or, for
   `orthology.tsv`, for the whole Version) — except `jbrowse.track`, which allows
   concurrent tracks.
2. On upload completion (`PreFinishResponseCallback` / `handlePreFinish`), the file is
   gzip-magic-byte verified, moved from the tus staging dir into
   `{uploadDir}/{version}/{species}/{fileName}` (flat `{uploadDir}/{version}/{fileName}`
   for `orthology.tsv`), and one or more `entity.Job` rows are enqueued in MySQL
   (status `PENDING`) via `enqueueProcessJob`, each with `AssemblyVersionID` set
   (`nil` only for `orthology.tsv`). Job payloads are typed structs in
   `internal/pkg/jobpayload/*`, JSON-marshaled into `Job.Payload`.
3. `cmd/worker` polls MySQL for pending jobs (`ClaimNextPending`), dispatches to a
   `map[jobType]Handler` in `internal/pkg/usecase/worker/handlers/`, marks the job
   `DONE`/`FAILED`, and calls optional `OnCompleteHook`/`OnFailureHook` on the handler.
   A separate ticker (`runStuckJobRecovery`) requeues jobs stuck in `RUNNING` past a
   configured timeout back to `PENDING`.
4. Some uploads implicitly enqueue **multiple** jobs / chain later jobs based on other
   jobs' state — this cross-job logic lives in `usecase/upload/upload.go`, not in the
   worker: e.g. uploading `genomic.gff` also enqueues a `SPECIES.SYNONYM` job tagged
   with the *uploading assembly's own* species (not a global config value);
   `GENOMIC.GFF:SETUP_JBROWSE2` is only enqueued once the corresponding
   `GENOMIC.FNA:SETUP_JBROWSE2` job for that *same assembly* is confirmed `DONE`
   (checked via `tryEnqueueGFFSetupJBrowse2`, both the upload-time copy in
   `usecase/upload` and the FNA-completion-triggered copy in
   `usecase/worker/handlers/setup_jbrowse2.go`), since the FNA (assembly) track must
   exist in JBrowse2 before the GFF (annotation) track can be added.
5. Job types are all constants in `entity/job.go` (`JobType*`), each with a
   human-readable entry in `JobDescriptions`. `:DELETE` and `:SETUP_BLAST` /
   `:SETUP_JBROWSE2` suffixes denote job variants derived from a base file type, not
   separate upload types.

`ReleaseVersion` (`usecase/version/version.go`) is the other job-enqueuing entrypoint
(besides upload completion): once per Assembly Version under the released Version, it
builds BLAST databases for `genomic.fna`/`protein.faa`/`rna.fna` via `*_SETUP_BLAST`
jobs. BLAST DB paths are **per-assembly, flat filenames** under one shared
`blast.db_path` (`{blast.db_path}/{AssemblyID}-{genome,protein,rna}`, where `AssemblyID`
is `entity.AssemblyVersion.AssemblyID()`) — every assembly under the default Version is
simultaneously BLASTable, with no "chosen species" concept. An assembly that omits an
optional file type (`protein.faa`/`rna.fna`) enqueues a `*_REMOVE_BLAST` job for that
assembly instead of silently leaving a previous release's database in place — see
`handlers/blast_shared.go` for the shared `finalizeBlastRelease` logic (used by both
`SetupBlastHandler` and `RemoveBlastHandler`) that waits for every setup/remove job of
the Version to finish, then promotes it to default, removes the *previous* default
Version's now-stale `{blast.db_path}/v{oldVersionID}a*` files (paths are no longer 3
shared slots a re-release overwrites for free), restarts the `blast` container, and
promotes every one of its assemblies' JBrowse2 views to the front (see below).

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
completion, independent of whether/when a version is released — every Assembly Version
that ever uploads `genomic.fna` gets its own JBrowse2 assembly (keyed by its opaque
`AssemblyID`, not by Version or species name — those aren't guaranteed unique across
different Versions and aren't safe as shell/jq matching keys) and `defaultSession.views`
entry, regardless of release status. `setup_jbrowse2_gff.sh` additionally takes a
`DISPLAY_LABEL` argument (`"{versionName} — {assembly.name}"`) purely for the
human-facing track title, since `AssemblyID` itself isn't presentable. Nothing about a
view entry reflects "this is the currently active version" by itself, and old entries
are never removed just because a newer version is released (only explicit
Version/Assembly-Version deletion, via `delete_jbrowse_version.sh` keyed by
`AssemblyID`, removes one — deleting a whole Version loops that same single-assembly
script once per assembly it had). To keep the app's default assemblies findable,
`finalizeBlastRelease` (see above) calls `set_default_jbrowse2_view.sh` once per
assembly under a newly-promoted-to-default version (in descending-`AssemblyID` order, so
the final front-to-back order ends up ascending): each call moves that one assembly's
view to the front of `defaultSession.views` (so JBrowse2 opens the lowest-`AssemblyID`
one first with no explicit session — the raw JBrowse2 app served at `/jbrowse2/` via
nginx has no query params to select a view otherwise) and prunes any views whose
assembly no longer exists in `.assemblies`. It does not delete history — older
versions'/assemblies' tracks remain and are still manually selectable via JBrowse2's own
UI.

### Elasticsearch conventions

- Index templates (mappings/analyzers) are created once via `es:migrate`
  (`cmd/esmigrate/esmigrate.go`), applied as composable index templates matching
  `{prefix}-{type}-*` so any dynamically created index inherits them.
- Actual indices are per-version, created at ingest time by the relevant repo; for
  `genomic`/`sequence`/`synonym`/`dsrna` (not `orthology`, which stays Version-only)
  they're additionally per-species, with multiple species' concrete indices coexisting
  behind one shared `{prefix}-{type}-{versionSlug}` alias. `SetAlias`/`DeleteStaleIndexes`
  on these 4 repos take a `species` argument and scope their remove/delete actions to
  that species' own indices, so uploading one species never evicts or deletes another's
  — `indexname.FromSpecies` sanitizes species codes into valid (lowercase) index name
  components, mirroring `indexname.FromVersionName`.
- ES 8 rejects wildcard patterns in `Indices.Delete` by default
  (`action.destructive_requires_name`). Always resolve wildcards with `Indices.Get`
  first, then delete by explicit resolved names — see `esindex.DeleteIndexesByVersion`
  (whole Version, all 5 types) and `esindex.DeleteIndexesByAssemblyVersion` (one
  species, the 4 assembly-scoped types only) for the reference pattern. Never call
  `Indices.Delete` with a wildcard string directly.

### Auth

`internal/pkg/auth` validates Keycloak-issued JWTs (via `lestrrat-go/jwx`) and enforces
a required realm role. `KEYCLOAK__DEV_BYPASS_AUTH=true` skips signature verification
entirely for local development — never rely on it being enabled in any deployed
environment.

### API docs

`docs/openapi.yaml` is the OpenAPI spec, embedded via `docs/embed.go` and served at
`/docs` (Swagger UI) and `/docs/openapi.yaml`. Update this spec when adding/changing
routes in `api/router.go`.
