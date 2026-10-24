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
| `GET /versions/{name}/orthologies?page=&page_size=` | Paged `orthology.tsv` uploads of the Version, newest first: `{ files: FileDetail[], total, page, pageSize }` (defaults `page=1`, `page_size=20`, max 100). Same entries as `orthologyTSV` in `GET /versions/{name}/detail`, which still returns them all for now. |
| `GET /versions/{name}/assemblies` | List assemblies; each item has its own `status`. |
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
- The `409` "job already pending or running" check is now per assembly (per Version for `orthology.tsv`), so two species can upload the same file type concurrently.
- `dsrna.csv` gate is now per assembly: the assembly's `speciesCode` must be `Tcas` (was: global `main_species`). Message changed to `dsrna.csv uploads are only supported for the "Tcas" species`.
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
| — | `orthologyTSV: FileDetail[]` (top level, shared). Unpaged; will be removed once the frontend uses `GET /versions/{name}/orthologies` |
| — | `assemblies: [{ id, versionId, name, speciesName, speciesCode, …, status, files }]` — `files` has the same per-type buckets minus `orthology.tsv`, plus `species.synonym[]` |
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
| `GET /versions`, `GET /public/versions` | `status` (and the `?status=` filter) uses the rolled-up status above. |
| `GET /versions`, `GET /versions/{name}/detail` | No longer need a token (were admin-only). Responses are unchanged, so they include `createdBy`/`updatedBy`, file paths and job errors. |
| `GET /search` | `orthologs` groups are sorted alphabetically by species (was: main species first). A direct `genes` hit is still only returned for the configured `main_species` (`Tcas`); a hit in any other species comes back as `orthologies` when it is in an orthology file, else as `otherGenes` with its `species` (e.g. `{ "species": "Lyst", "gene": "GSLYS_00000001001" }`). |
| `DELETE /versions/{name}` | Also cascades to all assemblies and their JBrowse2 data. |
| `GET /genes/{species}`, `GET /orthology/{species}` | No request/response change; reads now span every species' index in the Version. |

## Deprecated

| Endpoint | Use instead |
|---|---|
| `GET /public/versions` | `GET /versions` (now public, paged, same `status`). Kept until the frontend has migrated, then removed. |

## Unchanged

`POST /versions`, `GET /jobs`, `GET/DELETE /upload-files`, `/uploads/{id}` (PATCH/HEAD/DELETE), `/health`, `/docs`.

`GET /silencingseqs` is untouched and still gated on the global `main_species == "Tcas"` config; its multi-species behavior is deferred.

## Not available yet
- `GET /upload-files` does not report which assembly a file belongs to, and has no `assembly=` filter.
