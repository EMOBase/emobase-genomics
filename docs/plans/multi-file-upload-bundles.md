# Plan: Multi-file upload bundles (orthology + JBrowse2 tracks)

Status: implemented (see "Implementation notes" at the end for where the code
differs from this plan). End-to-end verification on the docker compose stack
(step 12) is still pending.

## Goal

Let an admin upload many `orthology.tsv` files, or many `jbrowse.track` files, in one
upload. The upload is a single `.tar.gz` archive that contains the data files and a
`manifest.csv` describing each file's metadata. These are the only two file types that
can have more than one file per version today.

Out of scope for v1: bundles for any other file type, and bundles that mix types.

## Agreed decisions

| Topic | Decision | Why |
|---|---|---|
| Bundle types | Two separate types: `orthology.bundle`, `jbrowse.track.bundle`. Each has its own job type and its own manifest template. | Simpler than one generic mixed-type bundle. More types can follow the same pattern later. |
| Archive format | Outer file is `.tar.gz`. Files inside can be plain or already gzipped. The server gzips plain files and copies gzipped files (detected by magic bytes) unchanged. | A gzip file holds only one file. Every worker handler reads input through `gzip.NewReader`, so stored files must be `.gz`. |
| Manifest | `manifest.csv` at the archive root, one column per metadata field (no JSON cells). The version comes from the upload's tus metadata, not the CSV. | Easier for users to fill in. One bundle covers one version. |
| Processing | Async. `handlePreFinish` stays the same and enqueues a bundle job. A worker extracts the archive and creates the child files and jobs. | Fits the existing upload → job pipeline. Large archives don't block the final tus request. |
| Children | Each extracted file gets its own `upload_files` row and normal job (`ORTHOLOGY.TSV` / `JBROWSE.TRACK`). After extraction, children are independent: listed, deleted and processed exactly like single uploads. | `Job.FileID`, `ListByVersion` and `DeleteFile` all work per file and stay unchanged. |
| Bundle row | Kept in `upload_files` as an audit record (who uploaded the bundle). Filtered out of version-level queries. It is not deletable. | Audit trail. It also supplies `CreatedBy` for the children. |
| Validation | All-or-nothing. If any row or file is invalid, no children are created, and the job fails listing every error. | Avoids half-imported bundles. |

## Finding: concurrent orthology jobs are safe

A bundle enqueues N orthology jobs for the same version at once. Single uploads currently
block this with `HasActiveJobOfType`. Reading the code (not load-tested) shows that
concurrent runs are safe:

- **One index per version, created safely.** All jobs for a version write to the same
  index, whose name is fixed by `version.CreatedAt`
  ([orthology_tsv.go:60-63](../../internal/pkg/usecase/worker/handlers/orthology_tsv.go#L60-L63)).
  The first bulk write auto-creates it from the `es:migrate` template, and Elasticsearch
  handles two writers auto-creating the same index at once.
- **No overwrites between files.** Document IDs are `{fileID}:{group}`
  ([orthology.go:9-11](../../internal/pkg/entity/orthology.go#L9-L11)), so files never
  overwrite each other's documents.
- **No shared deletes.** `Load` only writes. `OnFailure` and the delete job remove only
  their own `file_id`.
- **The alias race is harmless.** `SetAlias` reads the alias and then updates it in a
  separate call. Every job points the alias at the same index, so any interleaving ends
  in the same state.
- **No double claims.** `ClaimNextPending` uses `FOR UPDATE SKIP LOCKED`.

Two caveats, neither new: search can show partly loaded data while a job runs (this
already happens with sequential uploads), and N bulk loads at once add Elasticsearch
load (worker concurrency caps it).

## Implementation steps

### Phase 1: Foundations (no behaviour change)

1. **Entity constants.**
   - In [upload_file.go](../../internal/pkg/entity/upload_file.go): add
     `FileTypeOrthologyBundle = "orthology.bundle"`,
     `FileTypeJBrowseTrackBundle = "jbrowse.track.bundle"`, and a `BundleFileTypes` list.
   - In [job.go](../../internal/pkg/entity/job.go): add
     `JobTypeOrthologyBundle = "ORTHOLOGY.BUNDLE"` and
     `JobTypeJBrowseTrackBundle = "JBROWSE.TRACK.BUNDLE"`, plus `JobDescriptions` entries.

2. **New shared package `internal/pkg/uploadspec`.** Both the upload use case and the
   worker import it. For each child type it holds:
   - `ManifestColumns`:
     - orthology: `fileName,order,algorithm`
     - jbrowse: `fileName,trackName,category,selectInDefaultSession`
   - Validators `ValidateOrthologyMeta(map[string]string) error` and
     `ValidateJBrowseTrackMeta(...)`, moved out of `handlePreUploadCreate`.
   - Job builders `BuildOrthologyTSVJob(...)` and `BuildJBrowseTrackJob(...)`, which
     return an unsaved `*entity.Job`. Moved out of `enqueueProcessJob`.

   Switch `handlePreUploadCreate` and `enqueueProcessJob` to call these. A tus metadata
   map and a manifest row are both `map[string]string`, so one signature fits both.

3. **Allow concurrency.** At
   [upload.go:178](../../internal/pkg/usecase/upload/upload.go#L178), exempt
   `orthology.tsv` and both bundle types from the `HasActiveJobOfType` check.
   `jbrowse.track` is already exempt.

**Checkpoint:**
- `go build ./... && go vet ./...` passes.
- Single-file orthology and track uploads behave exactly as before.
- Two orthology uploads to one version can now run at the same time.

### Phase 2: Accepting bundle uploads

4. **Upload validation.** Add both bundle types to `allowedFileTypes`
   ([config.go](../../internal/pkg/usecase/upload/config.go)). In `handlePreUploadCreate`,
   require bundle file names to end in `.tar.gz`. Bundles need no other metadata.

5. **Bundle job: no new enqueue code.** `enqueueProcessJob`'s default branch already
   creates a job of type `strings.ToUpper(fileType)` (that is, `ORTHOLOGY.BUNDLE`) with
   `ProcessPayload{UploadFileID, VersionID, FilePath}`. That's all the handler needs. A
   `.tar.gz` already passes the existing `.gz` extension check and the `isGzip`
   magic-byte check.

6. **Hide the bundle row.** In
   [uploadfile/mysql.go](../../internal/pkg/repository/uploadfile/mysql.go), add
   `AND file_type NOT IN (<bundle types>)` to these queries:
   - `ListByVersionID`: the upload-files list.
   - `TotalFileSizeByVersionIDs`: otherwise the archive and its children are counted
     twice.
   - `FindLatestCompletedPerTypeByVersionID`: otherwise the version's file list in
     [version.go](../../internal/pkg/usecase/version/version.go) includes bundle types.

### Phase 3: Template endpoint

7. **Route.** Add `GET /upload-files/templates/:fileType` to the authenticated group in
   [router.go](../../internal/pkg/api/router.go).
   - It returns a CSV with only the header row, built from
     `uploadspec.ManifestColumns`.
   - It is served as `text/csv` with
     `Content-Disposition: attachment; filename={fileType}.manifest.csv`.
   - An unknown `fileType` returns 400.
   - The route is not under `/uploads`, because tus owns that path.

8. **OpenAPI.** Update [openapi.yaml](../openapi.yaml):
   - the new route,
   - the two bundle `fileType` values,
   - what each manifest column means,
   - the archive rules (see step 9).

### Phase 4: Worker handler

9. **`internal/pkg/usecase/worker/handlers/bundle.go`.** A single `BundleHandler`,
   configured per bundle type (child file type, manifest columns, validator, job
   builder). It depends on `uploadFileRepo`, `jobRepo` and `versionRepo`. Register it
   twice in `jobHandlers` in [worker.go](../../cmd/worker/worker.go). `Handle` runs these
   steps:

   1. **Load.** Read the payload, the version, and the bundle `upload_files` row (for
      `CreatedBy`).
   2. **Extract.** Stream the archive into `{uploadDir}/{version}/{bundleFileID}/`.
      - Reject any entry that isn't a regular file, or whose name contains `/`, `..` or
        an absolute path.
      - Keep `manifest.csv` in memory; don't write it to disk.
      - For every other entry, read its first 2 bytes. If they are the gzip magic
        bytes, copy the entry unchanged; otherwise gzip it.
      - The stored name is the original name with `.gz` added, unless it already ends
        in `.gz` or `.gzip`.
      - Write each file to `name.tmp`, then rename it. A rerun then never truncates a
        file that a child job is reading.
   3. **Validate everything, and report every error together.** On any error, remove
      the bundle folder and fail the job.
      - `manifest.csv` exists, and its header exactly matches `ManifestColumns`.
      - Each row passes the shared validator.
      - `fileName` is unique in the manifest.
      - The manifest's file names exactly match the archive's files (none missing,
        none extra).
      - `trackName` (jbrowse) or `order` (orthology) is unique within the bundle.
   4. **Create the children idempotently, one file at a time.**
      - `childID = uuid.NewSHA1(ns, bundleID + "/" + fileName)`. It is 36 characters,
        which fits `VARCHAR(36)`, and is the same on every rerun.
      - If `uploadFileRepo.FindByID(childID)` finds nothing, create the row: the child
        file type, the stored path and size, status `COMPLETED`, and the bundle's
        `CreatedBy`.
      - If `jobRepo.FindLatestByFileAndType(childID, jobType)` finds nothing, create the
        job with the shared builder.
      - This makes a requeue by `runStuckJobRecovery` safe: it creates no duplicates.
   5. **Finish.** Delete the archive. Return `{childFileIDs, jobIDs}` as the job result.

10. **Dependency.** Promote `github.com/google/uuid` from indirect to direct with
    `go mod tidy`.

### Phase 5: Tests and verification

11. **Unit tests.** These are the first `_test.go` files in the repo. Each test checks a
    rule for the reason given:
    - Path-traversal entries (`../x`, `/abs`, symlinks) are rejected, so nothing is
      written outside the upload folder.
    - A pre-gzipped file is not gzipped again; otherwise `gzip.NewReader` in the child
      handler would return compressed bytes.
    - Header mismatches, missing or extra files, duplicate `trackName`/`order`, and a
      non-integer `order` are all reported, and no children are created. A bad row
      can't produce a partial bundle.
    - Child IDs are deterministic, so a rerun doesn't duplicate rows or jobs.

12. **End-to-end test on the docker compose stack.**
    - **Orthology bundle** with 3 files, one of them already gzipped:
      - 3 child rows appear in `GET /upload-files`, and 3 `ORTHOLOGY.TSV` jobs reach
        `DONE`.
      - Each `file_id` has its own documents in Elasticsearch.
      - Deleting one child removes only that child's documents.
      - The bundle row is absent from the upload-files list and from the version's
        file size.
    - **Track bundle** with 2 files: both tracks appear in the JBrowse2 `config.json`.
    - **Broken bundle:** the job is `FAILED` and lists every error. No children exist,
      and the bundle folder is gone.
    - **Regression:** single-file uploads of every type still work.

## Resolved questions

1. **Archives created from a folder:** rejected, with an error that explains how to
   build the archive (`COPYFILE_DISABLE=1 tar czf bundle.tar.gz -C <folder> .`).
   The `./` root entry and `./` name prefixes that this command produces are accepted.
2. **Archive deleted on success:** yes. The bundle row's `FilePath` may point to a
   file that no longer exists; the row is an audit record only.

## Implementation notes (differences from the plan above)

- **No unique `order` check for orthology bundles.** `order` and `algorithm`
  together form a shared source label (`{order}.{algorithm}`, used as the group
  prefix and as the search source filter), so several files from one source can
  share an `order`. Single uploads don't enforce uniqueness either. Only
  `trackName` is checked for uniqueness within a jbrowse bundle.
- **Header columns can be in any order.** The header must contain exactly the
  template's columns, but their order doesn't matter. A UTF-8 BOM (as written by
  Excel) and trailing blank rows are tolerated.
- **Two passes over the archive.** Pass 1 validates every entry and the manifest,
  and reads to the gzip trailer so a truncated archive fails its checksum, all
  without writing anything. Pass 2 extracts. This means validation failures never
  need cleanup.
- **Stored-name collisions are rejected.** Example: `a.tsv` (gzipped on extraction
  to `a.tsv.gz`) together with `a.tsv.gz` in the same archive.
- **The archive is deleted in `OnComplete`, not in `Handle`.** If the worker dies
  before the job is marked `DONE`, the requeued job can still read the archive.
- **Child rows use `Create` (status `UPLOADING`) followed by `UpdateStatus(COMPLETED)`.**
  `Create` doesn't set `completed_at`. A rerun completes any child left half-created.
- **No new `enqueueProcessJob` branch.** A bundle job is built by the default
  branch (`strings.ToUpper(fileType)` with `ProcessPayload`), as planned.
