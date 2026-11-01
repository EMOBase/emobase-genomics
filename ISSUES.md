# Known issues

Open issues found by reviewing `feat/multi-species-assembly-versions` at `db35525`
(2026-11-01). None of them is fixed yet. When one is fixed, remove it here; git history
keeps the record.

Each issue says how it was confirmed: **Confirmed** means read in the code (and tested where
noted); **Likely** means it depends on runtime behavior nobody has checked yet.

## High

### H1. Deleting a version also deletes indexes of versions whose name starts the same way

- **Where:** `internal/pkg/repository/esindex/es.go:28` (`DeleteIndexesByVersion`).
- **Problem:** it deletes the indexes matching `{prefix}-{type}-{slug}-*`. Version names may
  contain `-`, so deleting `v1` also matches every index of a version named `v1-test`
  (`…-synonym-v1-test-v2a1-…`), across all 5 index types.
- **Impact:** deleting one version silently wipes another version's search data. That
  version's rows, files and JBrowse2 data stay, so it looks intact, but its searches return
  nothing.
- **Fix idea:** match by version ID instead of name. The 4 assembly-scoped types carry
  `v{versionID}a` (`{prefix}-{type}-{slug}-v{id}a*`), and orthology is exactly
  `{prefix}-orthology-{slug}-{createdAt}`. Or delete the indexes behind the exact alias
  `{prefix}-{type}-{slug}`.
- **Status:** Confirmed (code).

### H2. One failed BLAST job stops the version from ever becoming the default

- **Where:** `internal/pkg/usecase/worker/handlers/blast_shared.go:40`,
  `internal/pkg/usecase/version/version.go:715`.
- **Problem:** promotion waits until no BLAST job of the version is unfinished, and a FAILED
  job from an earlier attempt counts as unfinished. Nothing retries or clears failed jobs.
  Re-releasing is refused ("file jobs not complete"), because the failed `SETUP_BLAST` sits on
  the latest file. Uploading a new file gets past that check, but promotion still sees the old
  failure. A failed `REMOVE_BLAST` has no file to re-upload at all.
- **Impact:** the only way out is a new version.
- **Fix idea:** `POST /jobs/{id}/retry` (admin, FAILED → PENDING); when a retried BLAST job
  finishes, it runs the promotion again. Or make both checks look only at the latest attempt
  per assembly and job type, like the status rule.
- **Status:** Confirmed (code). Workaround:
  `UPDATE jobs SET status='PENDING', started_at=NULL WHERE id=…`.

## Medium

### M1. Slow jobs run twice

- **Where:** `internal/pkg/repository/job/mysql.go:192` (`RequeueStuckJobs`), `:204`
  (`MarkDone`).
- **Problem:** any job `RUNNING` for longer than `jobs.stuck_timeout` (30 min) is put back to
  PENDING; nothing tells a slow job from a dead one. `MarkDone` and `MarkFailed` don't check
  the status, so the first run can mark the job DONE while the second run is still going.
- **Impact:** a large `makeblastdb`, JBrowse2 text index or GFF load can run twice at once:
  two indexes, racing alias switches, two writers of the same BLAST files.
- **Fix idea:** have running jobs update `updated_at` regularly and requeue on that; guard
  `MarkDone`/`MarkFailed` with `WHERE status='RUNNING' AND started_at = ?`.
- **Status:** Likely (depends on how long jobs take).

### M2. Draft data is public

- **Where:** `nginx/conf.d/default.conf:66`; the public `GET /versions` and
  `GET /versions/{name}/detail`.
- **Problem:** nginx serves every `.gz` under `/public/uploads/`, and version detail (public
  since this branch) returns each file's `filePath`, plus job payloads, job errors and
  `createdBy`. Separately, uploads to the default version show up in search at once, before
  any release.
- **Impact:** anyone can list versions and download unreleased files.
- **Fix idea:** decide what is public. For example, show only released versions publicly, or
  serve draft files through the API with auth.
- **Status:** Confirmed (code). May be intended.

### M3. Older failed files of multi-file types don't show in the assembly status

- **Where:** `internal/pkg/repository/job/mysql.go:291` (`StatusCountsByAssemblyVersionID`).
- **Problem:** the assembly status counts only the latest file of each type, also for
  `jbrowse.track` and `species.synonym`, which keep many files. A failed older track or
  synonym file leaves the status `READY`.
- **Impact:** hidden failures (the opposite of the stuck `ERROR` fixed in `db35525`).
- **Fix idea:** for multi-file types, count every file not deleted, as the shared orthology
  status does.
- **Status:** Confirmed (code).

### M4. Track uploads don't wait for the genome

- **Where:** `internal/pkg/usecase/worker/handlers/jbrowse_track.go:28`.
- **Problem:** a GFF waits for `GENOMIC.FNA:SETUP_JBROWSE2`, but a `JBROWSE.TRACK` job runs at
  once. Tested with JBrowse CLI 4.2.0: if JBrowse2 has no assemblies yet, `add-track` fails
  ("No assemblies found") and the job stays FAILED. Otherwise the track is added for an
  assembly that doesn't exist yet, and works once the genome is set up; a default-session view
  made for it can be pruned before the genome arrives.
- **Fix idea:** enqueue track jobs only once the assembly's FNA setup is DONE, as
  `tryEnqueueGFFSetupJBrowse2` does for the GFF.
- **Status:** Confirmed (code and CLI test).

### M5. A failed BLAST restart is invisible

- **Where:** `internal/pkg/usecase/assemblyversion/assemblyversion.go:428`
  (`removeBlastDatabases`).
- **Problem:** the `BLAST:RESTART` job has no file, so it counts toward no status. If it fails,
  BLAST keeps serving the deleted assembly until its next restart, and only `GET /jobs` shows
  the failure.
- **Fix idea:** the retry endpoint from H2, or count version jobs that have no file in the
  shared status.
- **Status:** Confirmed (code).

## Low

### L1. The upload folder can differ in case from the version name

- **Where:** `internal/pkg/usecase/upload/upload.go:971` (`storedPath`).
- **Problem:** files are stored under the version name as sent in the upload metadata, while
  the lookup ignores case (MySQL collation). `version=tcas 5.2` for the version `Tcas 5.2`
  goes into `tcas 5.2/`; deleting the version removes the files but leaves that folder. More
  likely now that names may mix case.
- **Fix idea:** use the resolved version's stored name.
- **Status:** Confirmed (code).

### L2. Assemblies of the same species return duplicates

- **Where:** `internal/pkg/usecase/search/search.go:210`, `internal/pkg/usecase/search/genes.go`.
- **Problem:** `/genes/{species}` and `/silencingseqs/{species}` read every assembly of the
  species in the version, so two assemblies with the same gene or dsRNA ID both come back.
- **Status:** Confirmed (code).

### L3. Synonym IDs collide across sources

- **Where:** `internal/pkg/entity/synonym.go:26` (`GetID`).
- **Problem:** synonym document IDs are `{gene}:{type}[:{synonym}]`, so a GFF and a
  `species.synonym` file (e.g. FlyBase) that both give a gene a `SYMBOL` overwrite each other;
  the last writer wins. Removing one file's documents can remove a symbol the other file also
  had. See A1.
- **Status:** Confirmed (code).

### L4. Replaced files are found with one-second precision

- **Where:** `internal/pkg/repository/uploadfile/mysql.go:332` (`ListIDsReplacedBy`).
- **Problem:** it compares `created_at`, a DATETIME in seconds. An older upload created in the
  same second as the new one isn't treated as replaced, so its ES documents stay. This fails
  safe: the new file's documents are never deleted.
- **Status:** Confirmed (code).

### L5. Stale-upload cleanup is off, and wrong when turned on

- **Where:** `internal/pkg/usecase/upload/upload.go:934` (`cleanStaleUploads`).
- **Problem:** it runs only if `uploads.stale_after` is set, which it isn't by default, so
  abandoned partial uploads are never removed. When set, it judges age by the `.info` file,
  which tusd 2.9.2 writes only when the upload is created (chunks don't touch it), so an upload
  still in progress after `stale_after` gets deleted.
- **Fix idea:** judge age by the data file's modification time.
- **Status:** Confirmed (code and tusd source).

### L6. Deleting a version while an upload is in progress

- **Where:** `internal/pkg/usecase/version/version.go:873` (`DeleteVersion`).
- **Problem:** the active-job check can't see uploads still in progress, since they have no
  jobs yet. If such an upload finishes after the version is deleted, it recreates the version
  folder and fails to enqueue its job (the version row is gone), leaving the file on disk.
- **Status:** Confirmed (code).

### L7. Bundle extraction has no size limit

- **Where:** `internal/pkg/usecase/worker/handlers/bundle.go:577` (`writeGzipped`).
- **Problem:** a crafted archive can fill the disk. Uploads are admin-only.
- **Status:** Confirmed (code).

### L8. Version-name uniqueness is checked only in the app

- **Where:** `internal/pkg/usecase/version/version.go:835` (`CreateVersion`).
- **Problem:** two admins creating `Tcas 5.2` and `tcas_5.2` at the same moment could both
  succeed and share indexes.
- **Fix idea:** store the index slug in a column with a unique index.
- **Status:** Confirmed (code).

### L9. The JBrowse CLI version isn't pinned

- **Where:** `Dockerfile:51` downloads the latest `@jbrowse/cli`.
- **Problem:** `delete_jbrowse_track.sh` relies on behavior tested with 4.2.0: `remove-track`
  exits 0 for an unknown track ID, and track configs list their files as `.uri`. A new CLI
  release could change either.
- **Fix idea:** pin the version.
- **Status:** Confirmed (code).

### L10. Files of tracks deleted before `db35525` are still public

- **Where:** `/web/data` (served under `/jbrowse2/`).
- **Problem:** those deletions left the copied data files behind, and their config entries are
  gone, so nothing records which files they were.
- **Fix idea:** a one-off cleanup that compares `/web/data` against the `uri`s in
  `config.json`.
- **Status:** Confirmed (code).

### L11. A failed sequence re-upload leaves a mix

- **Where:** `internal/pkg/usecase/worker/handlers/sequence_fasta.go` (no `OnFailure`).
- **Problem:** if an `rna.fna`, `cds.fna` or `protein.faa` re-upload fails partway, the index
  holds the new file's sequences up to the failure point and the old ones after it, until the
  next successful upload of that type cleans up.
- **Status:** Confirmed (code).

### L12. CORS allows every origin

- **Where:** `internal/pkg/api/middleware/cors.go:18`.
- **Problem:** mostly harmless, since auth is a bearer header and not a cookie. Confirm it is
  intended.

## Needs a run on the real stack

These pass the unit tests but have never run against real MySQL, Elasticsearch or Docker
(Docker was down while they were written):

- **MySQL:** the status queries `StatusCountsByAssemblyVersionID` and
  `StatusCountsForSharedJobs` (window functions, `NULL` partitions, a scalar subquery);
  `HasActiveJobForFileTypeInAssemblyVersion` (join); `ListIDsReplacedBy` (self-join);
  `ListByVersionIDAndType` with `LIMIT 9223372036854775807`; `ListNames`.
- **Elasticsearch:** sequence `DeleteReplaced` and synonym `DeleteByFileIDs` (refresh, then
  `delete_by_query?conflicts=proceed`), on new indexes and on existing ones where `file_id` is
  mapped automatically as text. Re-run `es:migrate` first.
- **End to end:** re-upload a single-file type (the old file and its old ES documents go);
  delete an assembly of the default version (`BLAST:RESTART` restarts the container through
  the Docker socket); delete a track (`delete_jbrowse_track.sh` with real `flock` in the worker
  image).

## Accepted decisions

### A1. A failed GFF re-upload leaves a synonym gap

If a re-uploaded GFF's `SPECIES.SYNONYM` job fails, the genes it had already rewritten lose
their synonyms: its `OnFailure` deletes them by file ID, and synonym IDs don't include the
file. Accepted on 2026-11-01: re-upload until a GFF succeeds, which restores them. A real fix
would put `file_id` into the synonym ID, which needs a rule in `genes.go` for which source's
`SYMBOL`/`NAME` wins.
