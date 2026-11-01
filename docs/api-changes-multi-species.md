# API changes: single-species → multi-species

A **Version** (Database Version) now holds one or more **Assembly Versions**.
An Assembly Version is addressed by its numeric `id`. Its `speciesName` (e.g. `Homo sapiens`) and the
`speciesCode` derived from it (e.g. `Hsap`) describe the assembly but do not identify it, so one Version
can hold several assemblies of one species.
Orthology is the one exception: `orthology.tsv` and `orthology.bundle` stay shared across the whole Version.

Every `GET` under `/versions` is public (no token): `GET /versions`, `GET /versions/{name}/detail`,
`GET /versions/{name}/orthologies`, `GET /versions/{name}/assemblies`, `GET /versions/{name}/assemblies/{id}`.
Every other new endpoint below is admin-only. Apart from those GETs, endpoints that needed a token before still do.

## New endpoints

| Endpoint | Purpose |
|---|---|
| `GET /versions/{name}/orthologies?page=&page_size=` | Paged `orthology.tsv` uploads of the Version, newest first: `{ files: FileDetail[], total, page, pageSize }` (defaults `page=1`, `page_size=20`, max 100). The only way to list them: `GET /versions/{name}/detail` does not return them. |
| `GET /jobs/{id}` | One job, e.g. an id from an upload's `X-Jobs` header, for polling: the same fields as a `GET /jobs` item plus `assemblyVersionId` (`null` for orthology), `createdAt`, `updatedAt`, `startedAt`, `completedAt`. `400` non-integer id, `404` unknown id. |
| `GET /versions/{name}/assemblies` | List assemblies; each item has its own `status`. |
| `GET /silencingseqs/{species}?ids=\|geneIds=&version=` | One species' dsRNA silencing sequences; same query params and response as `GET /silencingseqs`. Records and genes of other species are never returned, even when they use the same dsRNA ID. Public. |
| `POST /versions/{name}/assemblies` | Create. Body `{ "name": "Human / GRCh38", "speciesName": "Homo sapiens", "infoUrl"?: "https://…" }` → `201`. `speciesCode` is derived: first letter of the first word (uppercased) + first 3 letters of the second (lowercased), e.g. `Tribolium castaneum` → `Tcas`; `400` if `speciesName` is not at least two such words. Several assemblies may share a species in one Version. |
| `GET /versions/{name}/assemblies/{id}` | Assembly detail: `status` + per-file-type `files`. `404` if the id is not in this Version. |
| `PATCH /versions/{name}/assemblies/{id}` | Partial update of `name` and/or `infoUrl` (`""` removes the link) → `200` with the assembly. `speciesName`/`speciesCode` cannot be changed (`400`). |
| `PUT /versions/{name}/assemblies/{id}/image` | `multipart/form-data` field `image`: PNG, JPEG or WebP, max 5 MiB → `200` with the assembly. Replaces any previous image. `400` wrong type, `413` too large. |
| `DELETE /versions/{name}/assemblies/{id}/image` | `204`. Removes the image (also when there is none). |
| `DELETE /versions/{name}/assemblies/{id}` | `204`. `422` if the assembly has `PENDING`/`RUNNING` jobs. `404` if the id is not in this Version. Removes that assembly's files, jobs, ES indices and JBrowse2 data; never touches the shared `orthology.tsv`. |

Every assembly object (list, detail, version detail's `assemblies[]`, create/update responses) also has
`imagePath` (served by nginx at `/{imagePath}`, like `filePath`; new path on each upload) and `infoUrl`,
both `null` when unset.

## Breaking changes

### `POST /uploads` (tus) — new required metadata `assembly`
- `assembly` = the target assembly's numeric `id` (from `GET /versions/{name}/assemblies`), not its species code. Required for every `fileType` **except `orthology.tsv` and `orthology.bundle`** (which take none).
- `jbrowse.track.bundle` covers exactly one assembly, named by `assembly`; every track in it is filed under that assembly. Its `manifest.csv` columns are unchanged.
- New `400`s: `"<fileType>" uploads require an "assembly" metadata field`; `"assembly" metadata must be an assembly id, got "X"` (for example a species code); `assembly "X" not found in version "Y"` (unknown id, or an id from another Version).
- The `409` "job already pending or running" check is now per assembly, so two species can upload the same file type concurrently. It now blocks while any job works on one of the assembly's files of that type (before: only a job whose type equals the file type, which never matched `genomic.fna`). `jbrowse.track`, `orthology.tsv` and the bundle types are exempt. Message ends `for this assembly` (was `for this version`).
- `dsrna.csv` is accepted for an assembly of any species (was: only when the global `main_species` was `Tcas`); the `dsrna.csv uploads are only supported…` `400` is gone.
- `species.synonym` also accepts dsRNA-to-gene mappings named `dsrna_gene*` (as well as `ib_tc*`), 2 columns: dsRNA ID, gene ID. They fill `geneIds` in `GET /silencingseqs/{species}`.
- `species.synonym` keeps its own `species` field (which species the file describes) and now also needs `assembly`.
- Jobs in the `X-Jobs` response header gain `AssemblyVersionID` (`null` for `orthology.tsv` and `orthology.bundle`).

### `POST /uploads` (tus) — bundles may also be `.zip`
- `orthology.bundle` and `jbrowse.track.bundle` accept a `.zip` archive as well as `.tar.gz`, with the same `manifest.csv` inside.
- The `400` for a wrong extension now reads `<fileType> uploads must be a .tar.gz or .zip archive`.
- A `.zip` whose content is not a zip is refused with `422` `uploaded file is not a valid zip`.

### `GET /versions/{name}/detail` — response reshaped
| Before | After |
|---|---|
| `files: { genomic.fna, …, orthology.tsv[], jbrowse.track[] }` | `files` removed |
| `files.orthology.tsv[]` | Not returned; page them with `GET /versions/{name}/orthologies` |
| — | `assemblies: [{ id, versionId, name, speciesName, speciesCode, …, status, files }]` — `files` has the same per-type buckets minus `orthology.tsv`, plus `species.synonym[]` and `jbrowse.track.bundle[]` (bundle archives with their `JBROWSE.TRACK.BUNDLE` job; the extracted tracks stay in `jbrowse.track[]`) |
| — | `orthologyBundle: FileDetail[]` (top level, shared): orthology bundle archives with their `ORTHOLOGY.BUNDLE` job; the extracted `orthology.tsv` files are paged by `GET /versions/{name}/orthologies`. A failed bundle extracts nothing, so its job's `error` is the only place the failure shows |
| `status` from the whole Version's jobs | `status` rolled up across every assembly + orthology: `PROCESSING` > `ERROR` > `READY` > `DRAFT`. No file is required, so `MISSING_REQUIRED_FILE` now only means zero assemblies |

### `POST /versions/{name}/release` — same request, per-assembly behavior
- Still no body. Now builds BLAST DBs **for every assembly**; all species are BLASTable at once once the Version is default.
- `jobs` in the response are one set per assembly; job payloads carry `assembly_id` / `assembly_name`.
- `genomic.fna` is no longer required: an assembly may be released with any subset of files, or none. A missing `genomic.fna` / `protein.faa` / `rna.fna` enqueues a `GENOMIC.FNA:REMOVE_BLAST` / `PROTEIN.FAA:REMOVE_BLAST` / `RNA.FNA:REMOVE_BLAST` job for that assembly.
- New `422`: no assemblies (`required file not uploaded: no Assembly Versions exist for this Database Version`).
- The "jobs not complete" `422` now names the species code, e.g. `file jobs not complete: rna.fna (species Hsap)`. It is checked per assembly and for the shared orthology file.

## Same shape, changed behavior

| Endpoint | Change |
|---|---|
| `POST /versions` | `name` must be 1-64 characters of letters (any case), digits, spaces, `.`, `_` or `-`, starting with a letter or digit and not ending with a space; otherwise `400` with that rule as the message. A name that matches an existing version's ignoring letter case and counting spaces as `_` (`tcas_5.2` when `Tcas 5.2` exists) is `400 version already exists: …`, naming that version. Before, any name was accepted, including `.` or `..` (unsafe as the version's upload folder) and names that shared another version's Elasticsearch indexes. Existing versions are unaffected. |
| `GET /versions`, `GET /public/versions` | `status` (and the `?status=` filter) uses the rolled-up status above. |
| Every `status` (version, assembly) | Counts only the jobs that still describe the version, so a failure can be cleared: the latest attempt per file and job type, the latest file-less job per type (e.g. `REMOVE_BLAST`), no jobs of deleted files, and only the latest `orthology.bundle`. Before, any failed job ever (of a deleted `orthology.tsv`, a failed bundle, an old `REMOVE_BLAST`) kept `ERROR` for good. |
| `DELETE /versions/{name}/assemblies/{id}` | Also removes the assembly's BLAST databases; for the default version, enqueues a new `BLAST:RESTART` job (version-scoped, no assembly or file) so a worker restarts the BLAST server. |
| `DELETE /versions/{name}` | Also removes its assemblies' BLAST databases. |
| `DELETE /upload-files/{id}` (`jbrowse.track`) | Also removes the track's copied data files from JBrowse2 (before: they stayed publicly served under `/jbrowse2/`) and its default-session entry. |
| `GET /versions`, `GET /versions/{name}/detail` | No longer need a token (were admin-only). Responses are unchanged, so they include `createdBy`/`updatedBy`, file paths and job errors. |
| `GET /search` | `orthologs` groups are sorted alphabetically by species (was: main species first). A direct `genes` hit is still only returned for the configured `main_species` (`Tcas`); a hit in any other species comes back as `orthologies` when it is in an orthology file, else as `otherGenes` with its `species` (e.g. `{ "species": "Lyst", "gene": "GSLYS_00000001001" }`). |
| `DELETE /versions/{name}` | Also cascades to all assemblies and their JBrowse2 data. |
| Every response with a `filePath` | The stored file name now carries an 8-character tag from the upload ID before its first dot (`genomic.gff.gz` → `genomic-3fa85f64.gff.gz`), so same-named uploads no longer overwrite each other. Files uploaded before this change keep their old names. Strip `-` plus the tag to show the original name. |
| `POST /uploads` (tus) | When an upload of `genomic.fna`, `genomic.gff`, `rna.fna`, `cds.fna`, `protein.faa` or `dsrna.csv` completes, the assembly's previous files of that type are removed from disk and soft-deleted (gone from `GET /upload-files`, no longer in `totalFileSize`), whatever their name. A previous file with a `PENDING`/`RUNNING` job is kept until the next upload of its type. |
| `GET /search`, `GET /search/_suggest`, `GET /genes/{species}` | After a `genomic.gff`, `rna.fna`, `cds.fna` or `protein.faa` is replaced and the new file's jobs are done, genes, synonyms and sequences that only the replaced file had are no longer returned (before: they stayed searchable). |
| `GET /genes/{species}`, `GET /orthology/{species}` | No request/response change; reads now span every species' index in the Version. |
| `GET /jobs` | New optional `type` filter: one job type, exact match (e.g. `?version=v1&type=GENOMIC.FNA:SETUP_BLAST`). An unknown type is `400 invalid type filter`. Without `type`, every job is listed as before. |

## Deprecated

| Endpoint | Use instead |
|---|---|
| `GET /public/versions` | `GET /versions` (now public, paged, same `status`). Kept until the frontend has migrated, then removed. |
| `GET /silencingseqs` | `GET /silencingseqs/{species}`. The old route now means the configured `main_species` and no longer returns `400` for a non-`Tcas` `main_species`. Kept until the frontend has migrated, then removed. |

## Unchanged

`GET/DELETE /upload-files`, `/uploads/{id}` (PATCH/HEAD/DELETE), `/health`, `/docs`.

## Not available yet
- `GET /upload-files` does not report which assembly a file belongs to, and has no `assembly=` filter.
