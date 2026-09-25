# API changes: single-species → multi-species

A **Version** (Database Version) now holds one or more **Assembly Versions**, one per species.
An Assembly Version is addressed by its `species` code (e.g. `Hsap`), unique within a Version.
`orthology.tsv` is the one exception: it stays shared across the whole Version.

All new/changed endpoints below are admin-only (same auth as today's `/versions/*`).

## New endpoints

| Endpoint | Purpose |
|---|---|
| `GET /versions/{name}/assemblies` | List assemblies; each item has its own `status`. |
| `POST /versions/{name}/assemblies` | Create. Body `{ "name": "Human / GRCh38", "species": "Hsap" }` → `201`. `400` if `species` already exists in this Version. |
| `GET /versions/{name}/assemblies/{species}` | Assembly detail: `status` + per-file-type `files`. |
| `DELETE /versions/{name}/assemblies/{species}` | `204`. `422` if the assembly has `PENDING`/`RUNNING` jobs. Removes that species' files, jobs, ES indices and JBrowse2 data; never touches the shared `orthology.tsv`. |

## Breaking changes

### `POST /uploads` (tus) — new required metadata `assembly`
- `assembly` = the target assembly's `species` code. Required for every `fileType` **except `orthology.tsv`** (which takes none).
- New `400`s: `"<fileType>" uploads require an "assembly" metadata field`; `assembly "X" not found in version "Y"`.
- The `409` "job already pending or running" check is now per assembly (per Version for `orthology.tsv`), so two species can upload the same file type concurrently.
- `dsrna.csv` gate is now per assembly: the assembly's `species` must be `Tcas` (was: global `main_species`). Message changed to `dsrna.csv uploads are only supported for the "Tcas" species`.
- `species.synonym` keeps its own `species` field (which species the file describes) and now also needs `assembly`.
- Jobs in the `X-Jobs` response header gain `AssemblyVersionID` (`null` for `orthology.tsv`).

### `GET /versions/{name}/detail` — response reshaped
| Before | After |
|---|---|
| `files: { genomic.fna, …, orthology.tsv[], jbrowse.track[] }` | `files` removed |
| — | `orthologyTSV: FileDetail[]` (top level, shared) |
| — | `assemblies: [{ id, versionId, name, species, …, status, files }]` — `files` has the same per-type buckets minus `orthology.tsv`, plus `species.synonym[]` |
| `status` from the whole Version's jobs | `status` rolled up across every assembly + orthology: `PROCESSING` > `ERROR` > `MISSING_REQUIRED_FILE` > `READY` > `DRAFT`; zero assemblies → `MISSING_REQUIRED_FILE` |

### `POST /versions/{name}/release` — same request, per-assembly behavior
- Still no body. Now builds BLAST DBs **for every assembly**; all species are BLASTable at once once the Version is default.
- `jobs` in the response are one set per assembly; job payloads carry `assembly_id` / `assembly_name`.
- New `422`: no assemblies (`required file not uploaded: no Assembly Versions exist for this Database Version`).
- Existing `422` messages now name the species, e.g. `required file not uploaded: genomic.fna (species Hsap)`. Preconditions are checked per assembly and for the shared orthology file.

## Same shape, changed behavior

| Endpoint | Change |
|---|---|
| `GET /versions`, `GET /public/versions` | `status` (and the `?status=` filter) uses the rolled-up status above. |
| `GET /search` | `orthologs` groups are sorted alphabetically by species (was: main species first). |
| `DELETE /versions/{name}` | Also cascades to all assemblies and their JBrowse2 data. |
| `GET /genes/{species}`, `GET /orthology/{species}` | No request/response change; reads now span every species' index in the Version. |

## Unchanged

`POST /versions`, `GET /jobs`, `GET/DELETE /upload-files`, `/uploads/{id}` (PATCH/HEAD/DELETE), `/health`, `/docs`.

`GET /silencingseqs` is untouched and still gated on the global `main_species == "Tcas"` config; its multi-species behavior is deferred.

## Not available yet
- `GET /upload-files` does not report which assembly a file belongs to, and has no `assembly=` filter.
