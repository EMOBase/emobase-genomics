# Multi-Species Assembly Versions — Technical Design Proposal

## Context

EMOBase Genomics today hardcodes a 1:1 relationship between a "Database Version" (the
`versions` MySQL table) and a single species, enforced not by schema but by a single
process-wide config string (`config.MainSpecies`). The user wants a Database Version to
hold data for **multiple species simultaneously**, each with its own full stack
(genomic, orthology, CDS, RNA, protein, etc.) and its own copy of today's per-file-type
upload validation — introducing a new "Assembly Version" entity as the per-species child
of a Database Version.

This document is a **design proposal only** — no code changes are made in this task. It
is organized in the 13 sections the user requested. Five scope-defining decisions were
confirmed directly with the user before/while drafting and are treated as fixed inputs,
not open questions:

1. **Species stays a free-form string** on the new Assembly Version entity — no new
   species registry/taxonomy table.
2. **BLAST becomes per-Assembly-Version, all simultaneously servable** — this decision
   was revised mid-design (an earlier draft of this document had it as a single
   admin-picked target; the user corrected this). The final, confirmed shape: every
   Assembly Version under the current **default** Database Version gets its own BLAST DB
   set, all coexisting and searchable in SequenceServer at once. There is no
   "target"/"primary" concept anywhere for BLAST. `app_settings.default_version_id`
   keeps its exact current meaning and target table — **it still points at a
   `versions.id`** (the Database Version), completely unchanged from today; it is not
   repointed to `assembly_versions` and no new pointer is introduced. See §5 for the
   per-assembly path scheme and the cleanup implications of dropping the "3 fixed global
   paths" assumption.
3. **Every Assembly Version is treated symmetrically in the data model** — no
   `is_primary`/"main"/"target" flag lives on the Assembly Version itself, and (per the
   BLAST decision above) none is needed anywhere else either. Release simply processes
   every Assembly Version under a Database Version identically.
4. **The existing Tcas-only `dsrna.csv`/silencing-seq restriction is preserved, not
   removed** — it is a real, still-needed product constraint for now. This design
   re-scopes it from a global config check to a per-Assembly-Version check (§3); removing
   it entirely is a separate future decision the product owner will make, not something
   this migration proposes or assumes.
5. **Clean cutover** — no production data must be preserved; migrations do not need a
   zero-downtime backfill path.

One further framing point, not asked as a question but treated as an assumption
throughout:
the "Database" at the top of the user's 3-level diagram (Database → Database Version →
Assembly Version) is **conceptual** — the whole single-tenant deployed instance (one
MySQL DB, one ES cluster already) — not a new table. Only "Database Version" (existing
`versions` table, unchanged) and the new "Assembly Version" need real schema.

All findings below were verified directly against the current source (exact file:line
citations throughout) — nothing here is inferred from naming conventions or CLAUDE.md
alone.

---

## 1. Current Architecture

### Species is not a data-model concept — it's a single config string

`entity.Version` (`internal/pkg/entity/version.go`) has exactly six fields: `ID, Name,
CreatedAt, CreatedBy, UpdatedAt, UpdatedBy`. No species field ever existed on it.

Species identity today comes entirely from `config.MainSpecies`
(`internal/pkg/config/config.go:16`), a single required, unvalidated free-text string
per deployment (`config.go:108-110` hard-fails at startup if empty). It's manually
threaded into ~8 usecase constructors at process start:
- `cmd/api/api.go:72,79` → `ucsearch.New`, `upload.New`
- `cmd/worker/worker.go:57,60,69` → `ucgenomic.New`, `ucsequence.New`, `ucdsrna.New`

It has **six** distinct consumption sites across the codebase (a sixth one is easy to
miss and is called out explicitly below because it will need re-scoping too):

1. **Gene-ID prefixing at ingest** — `mainSpecies + ":" + geneID` is baked into ES
   document IDs/fields in `usecase/genomic/genomic.go:88`, `usecase/dsrna/dsrna.go:86`,
   and `usecase/sequence/sequence.go:49`.
2. **dsRNA upload gate** — `usecase/upload/upload.go:104-107` rejects `dsrna.csv`
   uploads unless `mainSpecies == "Tcas"` (`entity.SpeciesTcas`).
3. **Silencing-seq search gate** — `usecase/search/search.go:214` rejects
   `GetSilencingSeqs` calls the same way.
4. **GFF → SPECIES.SYNONYM cross-job species arg** — `usecase/upload/upload.go:456`,
   the single most consequential call site (see below).
5. **`/search` main/other synonym split** — `search.go:103-124`,
   `mainPrefix := uc.mainSpecies + ":"`.
6. **`/search` orthology "main species first" ordering** — `search.go:185-190`,
   sorts ortholog groups so `groups[i].Species == uc.mainSpecies` comes first.

"Related species" (the *other* side of orthology/synonym relationships) have **never**
had a registry — they're already fully free-form, per-upload strings today, via two
existing mechanisms:
- `species.synonym` uploads require a `species` metadata string
  (`upload.go:158-163`), validated only for non-empty, carried into
  `jobpayload.SpeciesSynonymPayload.Species` and used verbatim as an ES gene-ID prefix.
- `orthology.tsv`'s **header row itself** declares species codes as column headers
  (`usecase/orthology/orthology.go:60`: `species := strings.Split(line, delimiter)[1:]`),
  and each ortholog is stored as `<species>:<geneID>` — orthology documents already
  routinely contain multi-species ID arrays like `["Dmel:FBgn0000001",
  "Tcas:TC016177"]` inside a single version's index, today, with zero schema support.

### MySQL schema — 4 tables, no species/assembly column anywhere

- `versions(id, name UNIQUE, created_at, created_by, updated_at, updated_by)` —
  `migrations/000001_create_versions.up.sql`.
- `upload_files(id VARCHAR(36) PK, version_id FK, file_path, file_type, metadata JSON,
  file_size, upload_status ENUM, created_at, created_by, completed_at, deleted_at,
  deleted_by)` — `migrations/000002...up.sql`. `entity.UploadFile.FileType` constants:
  `genomic.fna, genomic.gff, rna.fna, cds.fna, protein.faa, orthology.tsv,
  species.synonym, dsrna.csv, jbrowse.track`.
- `jobs(id, version_id FK, file_id FK nullable, type, description, payload JSON, status
  ENUM, result_metadata JSON, created_at, updated_at, started_at, completed_at)` —
  `migrations/000003...up.sql`. 19 `JobType*` string constants in `entity/job.go`.
- `app_settings(id, default_version_id FK nullable)` — single-row table, one global
  "default version" pointer (`repository/appsettings`, `SetDefaultVersion` /
  `GetDefaultVersionID`).

### Upload pipeline (tus-based, `internal/pkg/usecase/upload/upload.go`)

1. `handlePreUploadCreate` (89-197): validates `fileType` against a 9-entry allowlist
   (`upload/config.go:10-20`), the dsrna-requires-Tcas gate, `fileName` pattern, gzip
   extension, per-type metadata (orthology needs `order`+`algorithm`; genomic.gff needs
   `geneIDKey`+`trimPrefixChars`+`trimSuffixChars`; jbrowse.track needs `trackName`;
   species.synonym needs `species`), resolves `version` (a **name** string) via
   `versionRepo.FindByName`, rejects if an active job of the same fileType already
   exists for that version (`jobRepo.HasActiveJobOfType(versionID, fileType)`) — except
   `jbrowse.track`, which allows concurrent tracks. Stashes `_versionID` into metadata.
2. `onCreated` (205-238): creates the `upload_files` row, path =
   `filepath.Join(uploadDir, meta["version"], filepath.Base(meta["fileName"]))` — one
   flat directory per Database Version name.
3. `handlePreFinish` (240-304): verifies gzip magic bytes, moves
   `srcPath → {uploadDir}/{versionName}/{basename(fileName)}` (259-271), calls
   `enqueueProcessJob`, marks the upload `COMPLETED`.
4. `enqueueProcessJob` (306-472): branches by fileType. `genomic.fna` → only enqueues
   `GENOMIC.FNA:SETUP_JBROWSE2` (no parse job). `species.synonym` and `jbrowse.track`
   build their own payloads directly. Everything else builds `GenomicGFFPayload` /
   `OrthologyTSVPayload` / generic `ProcessPayload`, job type = `strings.ToUpper(fileType)`.
   **Critical cross-job block (452-468)**: every `genomic.gff` upload also calls
   `enqueueSpeciesSynonymJob(..., uc.mainSpecies, ...)` — tagging the auto-generated
   SPECIES.SYNONYM job with the **global** config species, regardless of which species
   the GFF actually describes — then `tryEnqueueGFFSetupJBrowse2` (551-625), which finds
   "the" latest completed `genomic.fna` **for the whole version**
   (`FindLatestCompletedByVersionAndType`, assumes exactly one FNA per version) and
   chains GFF's JBrowse2 setup job once FNA's setup job is DONE.

### Release / required-file validation (`usecase/version/version.go`)

`ReleaseVersion` (340-449): `FindLatestCompletedPerTypeByVersionID` returns one latest
file **per type for the whole version** (assumes one file per type per version — this
assumption breaks completely once 2 species can each have their own `genomic.fna`).
The sole "required file" check in the codebase: `if latestByType[FileTypeGenomicFNA] ==
nil { return ErrRequiredFileNotUploaded }` (359-361) — a hardcoded string-constant
check, 5 reference sites total (`version.go:168,261,282,359,384`; `upload.go:315`), no
config/data-driven "required" flag anywhere. A `blastSpec` table then enqueues
`SETUP_BLAST`/`REMOVE_BLAST` jobs for `genomic.fna`/`protein.faa`/`rna.fna`, payloads
carrying only `VersionName` (`SetupBlastPayload{FilePath, VersionName}`,
`RemoveBlastPayload{VersionName}`). Comment at 412-417 confirms BLAST DB paths are
global: **"re-releasing this version... needs to rebuild them from this version's own
files again"** — confirmed via `cmd/worker/worker.go:92-106`: exactly 3 hardcoded
global output paths (`blastDBPath+"/genome"`, `/protein`, `/rna`), shared by the whole
deployment; whichever version was last released overwrites them.

`computeVersionStatus` (313-328): `PROCESSING` if any job running/pending, else
`ERROR` if any failed, else `MISSING_REQUIRED_FILE` if no genomic.fna, else `READY` if
all jobs done, else `DRAFT`.

`finalizeBlastRelease` (`usecase/worker/handlers/blast_shared.go`), called from both
`SetupBlastHandler.OnComplete` and `RemoveBlastHandler.OnComplete`: once all 5
BLAST-related job types for a version are non-pending, it (1) sets the version as the
single global default (`appSettingsRepo.SetDefaultVersion`), (2) restarts the shared
blast Docker container, (3) promotes that version's JBrowse2 assembly to the front of
`defaultSession.views` via `set_default_jbrowse2_view.sh`.

### Elasticsearch — index naming and the alias mechanism

All 5 ES-backed repos (genomic, sequence, orthology, synonym, dsrna) build names the
same way in the worker handlers (`usecase/worker/handlers/{genomic_gff,sequence_fasta,
orthology_tsv,synonym,dsrna_csv}.go`): `aliasName = "{prefix}-{type}-{versionSlug}"`,
`indexName = aliasName + "-" + timestamp`. **No species dimension anywhere.**

Each repo's `SetAlias(ctx, indexName, aliasName)` (identical implementation across all
5, e.g. `repository/genomic/es.go:179-232`) does: look up **every** index currently
under `aliasName`, remove all of them, add the new one — its own doc comment says
*"removing it from any previous index it may have pointed to."* `DeleteStaleIndexes`
(`genomic/es.go:128-175`) uses pattern `aliasName + "-*"` and deletes every match except
the just-written live index. **Both are alias-scoped, i.e. version-scoped, with the
implicit assumption of exactly one live index per version per type** — the moment two
species share a Database Version and each re-upload their own `genomic.gff`, the second
upload's `SetAlias` call would remove the first species' index from the alias, and its
`DeleteStaleIndexes` call would delete it outright.

`esindex.DeleteIndexesByVersion` (`repository/esindex/es.go:28-86`) does GET-then-DELETE
(ES8 forbids wildcard `DELETE`) against 5 hardcoded `{prefix}-{type}-{versionSlug}-*`
patterns — version-scoped, deletes all data for a version in one call. Index templates
(`cmd/esmigrate/esmigrate.go`) are type-only wildcards (`{prefix}-{type}-*`), already
species-agnostic; the `sequence` template already has a per-document `species: keyword`
field.

### Search API — `/orthology/:species` and `/genes/:species` already take free-form species

`internal/pkg/usecase/search/genes.go` and `orthology.go`: the `:species` path param is
used raw as a literal ID-prefix (`species + ":" + gene`) against documents that already
store `Species:GeneID`-shaped IDs, with **zero validation against any whitelist**. These
endpoints already work for arbitrary species values today — species has always been a
de facto multi-valued dimension inside ES documents, even though `Version` never modeled
it. The one real gap is `/silencingseqs` (`search.go:213-232`), which takes **no**
species param at all — gated entirely by the global `mainSpecies == "Tcas"` check.

### JBrowse2 / BLAST filesystem integration

JBrowse2 assembly `.name` is **literally the Database Version name**, 1:1, hardcoded
across all 5 relevant scripts (`scripts/setup_jbrowse2_fna.sh`:
`jbrowse add-assembly ... --name "$VERSION"`; `setup_jbrowse2_gff.sh`:
`--assemblyNames "$VERSION"`, track titled `"${VERSION} Annotations"`;
`add_jbrowse_track.sh`: `ASSEMBLY_NAME` param = version name, on-disk files prefixed
`"$ASSEMBLY_NAME.$BASENAME"`; `delete_jbrowse_version.sh`: `jq` matches `.name ==
$version` everywhere; `set_default_jbrowse2_view.sh`: promotes the view whose
`init.assembly == $VERSION`). JBrowse2 **natively supports N simultaneous assemblies**
(its own `assemblies[]` array) — this codebase's automation just constrains it to
exactly 1-per-version by convention, not by any JBrowse2 limitation. The 3 relevant
payload structs (`SetupJBrowse2FNAPayload`, `SetupJBrowse2GFFPayload`,
`JBrowseTrackPayload`) all key solely off `VersionName`.

`setup_blast.sh`/`remove_blast.sh` are fully generic (`makeblastdb -in FILE -out OUT`,
`rm -f "${OUT}".*`) — no version/species baked in; the caller supplies every path.

### API surface (`internal/pkg/api/router.go`)

Public: `/health`, `/docs`, `/docs/openapi.yaml`, `/search`, `/search/_suggest`,
`/orthology/:species`, `/genes/:species`, `/silencingseqs`, `/public/versions`.
Admin-only (single flat `middleware.RequireAdmin` role, no finer-grained roles):
`/uploads` (tus), `/versions` (GET/POST), `/versions/:name/detail`, `/versions/:name`
(DELETE), `/versions/:name/release`, `/jobs`, `/upload-files` (GET/DELETE).
`VersionDetail`/`VersionDetailFiles` (`version.go:88-109`) is one flat bucket per file
type, no species dimension. `POST /versions` body is `{name}` only.

### No frontend in this repo

Confirmed backend-only Go module (`cmd/`, `internal/`, `migrations/`, `scripts/`,
`docs/`, `nginx/`, `data/`). `data/jbrowse2/` is the pre-built official JBrowse2 static
web bundle, not custom source. Any admin/consumer UI lives in a separate repository —
out of scope here.

---

## 2. Proposed Architecture / Data Model

### New table: `assembly_versions` — fully symmetric, no per-row "special" flag

```sql
CREATE TABLE assembly_versions (
    id          BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    version_id  BIGINT UNSIGNED                     NOT NULL,
    name        VARCHAR(255)                        NOT NULL,
    species     VARCHAR(255)                        NOT NULL,
    created_at  DATETIME                             NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by  VARCHAR(255)                         NOT NULL,
    updated_at  DATETIME                             NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    updated_by  VARCHAR(255)                         NOT NULL,

    CONSTRAINT fk_assembly_versions_version FOREIGN KEY (version_id) REFERENCES versions(id),
    UNIQUE KEY uq_assembly_versions_version_species (version_id, species)
);
```

Field naming and roles were revised to match this codebase's existing conventions
(an earlier draft used `species`/`slug` where `slug` was a *derived, sanitized* infra
key computed from free text — the user corrected this to match how species identity
already works everywhere else in this codebase): `name` is the free-text, human-readable
label (e.g. "Human / GRCh38"), and **`species` is a short, directly admin-provided code**
(e.g. `"Tcas"`, `"Dmel"`, `"Hsap"`) — the same shape of value already used throughout
the existing system (`entity.SpeciesDmel = "Dmel"`, `entity.SpeciesTcas = "Tcas"` in
`internal/pkg/entity/species.go`; the `species.synonym` upload's `species` metadata
field; `orthology.tsv`'s header-row species codes). Unlike the earlier draft's `slug`,
**`species` is not derived from `name`** — it's a distinct, directly-entered field, so
there's no sanitization/collapsing logic to design; `UNIQUE(version_id, species)` is a
plain exact-match uniqueness constraint (case-insensitive by MySQL's default collation),
not a collision-avoidance scheme for arbitrary free text.

Every Assembly Version row is identical in shape and status to every other — there is no
`is_primary`, no auto-assignment on creation, no arity-dependent branching anywhere. This
was a deliberate revision from an earlier draft of this design (which had an
auto-assigned `is_primary` flag); the user explicitly rejected that in favor of full
symmetry between assemblies.

### No BLAST/default-view target concept anywhere — `app_settings` is fully untouched

Because every Assembly Version under the default Database Version is now equally
BLASTable (§5), there is no "chosen species" to point at, and therefore nothing to add
or repoint. `app_settings.default_version_id` keeps its exact current column, FK target
(`versions(id)`), and meaning — "which Database Version is currently the app's
default/served version" — completely unchanged. `SetDefaultVersion`/
`GetDefaultVersionID` in `repository/appsettings` are untouched. Every existing caller —
`versionresolver.Resolver.Resolve`, `ListVersions`, `GetVersionDetail`'s `IsDefault`
computation, `DeleteVersion`'s `ErrCannotDeleteDefaultVersion` guard — needs **zero
changes**. This was a genuine mid-design correction: an earlier draft introduced a
repointed/renamed `app_settings` column to represent a single BLAST target, which is no
longer needed once BLAST itself became per-assembly rather than single-target.

### What changes on existing tables — additive, not a rename (revised: orthology is shared, not per-assembly)

An earlier draft of this design replaced `upload_files.version_id`/`jobs.version_id`
outright with `assembly_version_id`, on the assumption every upload belongs to exactly
one Assembly Version. The user corrected this: **orthology.tsv is shared across every
species in a Database Version, not owned by any single assembly** (orthology documents
inherently cross-reference multiple species already, via the header-row-declared species
codes — §1). That one file type has no assembly to attach to, so the schema change
becomes **additive** instead of a replacement:

- `versions` — **fully unchanged**.
- `upload_files` and `jobs` both **keep their existing `version_id` column exactly as
  today** (`NOT NULL`, unchanged meaning: "which Database Version") and **gain one new
  nullable column**, `assembly_version_id BIGINT UNSIGNED NULL FK → assembly_versions(id)`.
  Populated for every file type **except** `orthology.tsv`, which leaves it `NULL` —
  exactly mirroring how `orthology.tsv` already behaves today (Database-Version-scoped
  only, no narrower ownership).
- `app_settings` — **fully unchanged**, as above.

This is a real simplification over the earlier rename-based draft: every existing query
that filters by `version_id` (`FindByVersionID`, `HasActiveJobOfType`,
`TotalFileSizeByVersionIDs`, `StatusCountsByVersionID`, `ListByVersionID`,
`HasActiveJobsByVersionID`, `DeleteByVersionID`, `HardDeleteByVersionID`, ...) **needs no
changes at all** — they keep working exactly as today, and Database-Version-level rollup
queries need no new joins. Only the new per-assembly concerns (upload dedup scoped to
one species, per-assembly required-file checks, per-assembly detail) need the new
`assembly_version_id`-filtered query variants, added alongside the existing ones.

### Relationship diagram

```
versions (Database Version)                 1 ──< N   assembly_versions (Assembly Version)
  id, name, ...                                         id, version_id, name, species, ...

versions                                     1 ──< N   upload_files, jobs   (unchanged FK, all rows)
                                                          id, version_id, assembly_version_id (nullable), ...

assembly_versions                            1 ──< N   upload_files, jobs   (new FK; NULL for orthology.tsv rows)
                                                          id, version_id, assembly_version_id (nullable), ...

app_settings.default_version_id ─────────────────────→ versions.id   (unchanged, still Database-Version-level)
```

### Fields/behaviors explicitly NOT changing (call these out to the team)

- `entity.Version` struct — **fully unchanged**, no fields added, removed, or renamed.
- `app_settings` table, its columns, and `repository/appsettings`'s existing methods —
  **fully unchanged** (see above; this reverses an earlier draft's proposed rename).
- ES index **templates** (`cmd/esmigrate/esmigrate.go`) — fully unchanged; species
  identity for the 4 assembly-scoped types lives entirely in the index name (§5), no
  document-level field or template mapping needed.
- `esindex.DeleteIndexesByVersion`'s existing wildcard *pattern shape* — a
  version-scoped `-*` suffix still correctly matches every assembly's concrete indices
  once the assembly dimension is added only to concrete index names (see §5).
- `/orthology/:species`, `/genes/:species`, `/public/versions` — no structural change
  (see §5 for why, and the one ES correction that makes this true for the right reason).
- Public `versions` listing shape (`VersionPublicItem{id, name, createdAt}`).

---

## 3. Upload & Validation Flow

### Upload metadata: new required field — with one exception

Add a required tus metadata field **`assembly`** — the Assembly Version's **`species`
code** (string, e.g. `"Hsap"`, `"Mmus"`), not its numeric ID, consistent with how the
existing `version` metadata field is already the Database Version's `name` rather than
its numeric ID (`upload.go:166`). Not named `assemblyVersion` (avoids stutter) and kept
distinct from `species.synonym`'s own `species` metadata field even though both now hold
same-shaped short-code values: `species.synonym`'s field means "which species this
synonym data *describes*," which can legitimately differ from the assembly it's
organizationally filed under (e.g. filing Dmel synonym data under the Tcas assembly for
cross-referencing) — so a separate `assembly` key (meaning "which assembly bucket this
upload belongs to") stays correct even though it may often equal `species.synonym`'s
own `species` value in the common case. `species.synonym`'s existing field is unchanged.

**`orthology.tsv` is the one exception**: it does **not** take an `assembly` field at
all — it stays scoped only by `version` (the Database Version), exactly as it behaves
today, since orthology data is shared across every species in the version rather than
owned by one (§2). `handlePreUploadCreate`'s per-file-type metadata validation (§1) gains
one more type-conditional branch: require `assembly` for every `fileType` except
`orthology.tsv`.

`handlePreUploadCreate` (`upload.go:89-197`) gains a step 5b right after today's version
resolution (166-174), skipped for `orthology.tsv`: resolve `(versionID, assembly code)`
→ a concrete `assembly_version_id` via a new `assemblyVersionRepo.FindBySpecies(ctx,
versionID, species)` (the new primary lookup method, mirroring `versionRepo.FindByName`),
400/404 if missing or belonging to a different Database Version — same shape as today's
version-not-found check. Stash the resolved numeric `_assemblyVersionID` into metadata
alongside the existing `_versionID` (left unset for orthology uploads) — the `species`
code is only ever used for lookup/addressing; every internal FK (jobs, upload_files)
still stores the stable numeric ID.

### File path / dedup / cross-job re-scoping

- **File path**: `{uploadDir}/{versionName}/{fileName}` becomes
  `{uploadDir}/{versionName}/{species}/{fileName}` for every type **except
  `orthology.tsv`**, which keeps its current flat `{uploadDir}/{versionName}/{fileName}`
  path unchanged (nothing to disambiguate by species). Both `onCreated` (line 220) and
  `handlePreFinish` (259-271, including `os.MkdirAll`) branch on file type for this.
  This also fixes a pre-existing latent collision risk for the assembly-scoped types
  (same-named files from different uploads overwriting each other in one flat version
  directory), which multi-species would otherwise make load-bearing rather than
  accidental.
- **Active-job dedup** (`HasActiveJobOfType`, line 179): re-scopes its first argument
  from `version.ID` to the resolved `assemblyVersion.ID` for every type except
  `orthology.tsv`, which **keeps its existing `version.ID` scoping unchanged** — an
  in-flight `genomic.gff` job for species A no longer blocks species B's `genomic.gff`
  upload in the same Database Version, while orthology uploads continue to dedup at the
  whole-Database-Version level exactly as today.
- **GFF → SPECIES.SYNONYM cross-job** (line 456): stops passing `uc.mainSpecies`,
  passes the resolved Assembly Version's own `Species` field instead. This is the
  single most consequential code change in the whole design — it's what makes the
  auto-generated synonym job describe the *correct* species for a multi-species upload.
- **`tryEnqueueGFFSetupJBrowse2`'s FNA lookup** (line 552,
  `FindLatestCompletedByVersionAndType`): re-scopes from `versionID` to
  `assemblyVersionID` — now genuinely safe to assume "the" FNA for that lookup, since an
  Assembly Version *is* the new one-FNA-per-unit boundary (today's implicit
  one-FNA-per-version assumption was already fragile; this makes it correct by
  construction).
- **dsrna.csv Tcas gate** (line 104-107): re-scopes from `uc.mainSpecies != "Tcas"`
  (global) to the resolved Assembly Version's `Species != "Tcas"` (per-assembly, same
  hardcoded product rule, just correctly scoped). Confirmed with the user: this
  restriction is **preserved as-is**, not removed or questioned — it's a real,
  still-needed product constraint today. Removing it is a separate decision the product
  owner will make in the future, not something this migration proposes.
- Every `entity.Job{VersionID: ...}` / `entity.UploadFile{VersionID: ...}` literal in
  `upload.go` (7 job-construction sites: lines 337, 373, 429, 491, 525, 604, 712, plus
  the `UploadFile` literal at 217) additionally sets the new `AssemblyVersionID` field —
  `VersionID` itself is **kept, unchanged**, on every literal (§2's additive schema
  change); `AssemblyVersionID` is left `nil` only for the `orthology.tsv` branch.

### Required-file / release validation moves to per-assembly

Today's single check ("does the Database Version have a `genomic.fna`?",
`version.go:359-361`) becomes: **every existing Assembly Version under the Database
Version must have its own completed `genomic.fna`** before release is allowed, and a
Database Version with **zero** Assembly Versions cannot be released at all. This directly
satisfies the user's constraint that existing per-file-type rules continue to apply "per
Assembly Version."

`ReleaseVersion`'s **signature is unchanged** — no new required input, no
`blastAssemblyId` (an earlier draft of this design added one; removed once BLAST became
per-assembly rather than single-target, per the user's correction). Instead, the
existing `blastSpec`/`latestByType` loop (`version.go:378-446`) gains one outer loop:
run the *exact same* per-file-type logic (required `genomic.fna` → `SETUP_BLAST`;
optional `protein.faa`/`rna.fna` → `SETUP_BLAST` if present, `REMOVE_BLAST` if absent)
**once per Assembly Version** under the Database Version, instead of once for the whole
version. This is a minimal, mechanical extension of the existing function — the
per-file-type decision logic itself doesn't change at all, it's just invoked N times
instead of once. `SetupBlastPayload`/`RemoveBlastPayload` gain the assembly's opaque
`AssemblyID` (§5) alongside `VersionName`, since BLAST output paths are now per-assembly
rather than 3 fixed global ones.

### Status computation — Database-Version-level becomes a rollup

`computeVersionStatus` (313-328) is **unchanged** as a function — it still computes one
assembly's status from that assembly's own job/file state. It's now called once per
Assembly Version, **plus once more for orthology's shared status** (jobs where
`version_id = V AND assembly_version_id IS NULL`, `hasRequiredFiles` always `true` since
orthology was never a required file type), and the Database Version's status is a
rollup across all of them:

```
n = count(assembly_versions where version_id = V)
S_orthology = computeVersionStatus(orthology job counts for V, true)   // READY if none uploaded yet
if n == 0:                                     → MISSING_REQUIRED_FILE   (reuses the existing enum value)
else if any assembly == PROCESSING or S_orthology == PROCESSING → PROCESSING
else if any assembly == ERROR or S_orthology == ERROR           → ERROR
else if any assembly == MISSING_REQUIRED_FILE  → MISSING_REQUIRED_FILE
else if all assemblies == READY (and S_orthology == READY)      → READY
else                                            → DRAFT
```

Folding in `S_orthology` is what makes a still-processing or failed orthology upload
visible at the Database-Version level even though it belongs to no single assembly and
therefore can't surface through any individual assembly's status. Reusing
`MISSING_REQUIRED_FILE` for the zero-assembly case (rather than inventing a 6th status
string) keeps the enum stable for whatever consumes `VersionItem.Status`/
`VersionDetail.Status` outside this repo — but that's still a coordination point worth
flagging explicitly (§10), not assuming away. This rollup degenerates identically to
today's behavior for a Database Version with exactly one assembly and no orthology
upload in flight, so single-species deployments see no behavior change.

### Three levels of validation, summarized

| Level | Examples | Where enforced |
|---|---|---|
| **Database-Version-level** | Version name uniqueness; release requires ≥1 assembly; delete blocked if default or has active jobs; orthology.tsv upload/validation (shared, not owned by any assembly) | `usecase/version` |
| **Assembly-Version-level** | Required `genomic.fna` per assembly before release; species-code uniqueness within a version; delete has no BLAST/orthology-related guard at all — deleting one assembly never touches shared orthology data | `usecase/assemblyversion` (new) |
| **File-level** | Per-file-type metadata (geneIDKey, order, algorithm, trackName...), gzip check, dsrna Tcas gate, active-job-of-same-type dedup | `usecase/upload` (unchanged shape, re-scoped inputs except orthology.tsv) |

---

## 4. API Changes

### New endpoints (all admin-only, matching existing `/versions/*` placement)

- `POST /versions/:name/assemblies` — create `{name, species}` → `{id, versionId, name,
  species, createdAt, ...}` — no special/target field; every assembly is created equal.
  `id` is returned for internal/debugging reference, but every other endpoint below
  addresses assemblies by **`species`** (the short code), not `id` — consistent with how
  Database Versions are already addressed by `:name` rather than numeric ID throughout
  this API.
- `GET /versions/:name/assemblies` — list
- `GET /versions/:name/assemblies/:species` — detail (mirrors today's `VersionDetail`/
  `VersionDetailFiles` one level down)
- `DELETE /versions/:name/assemblies/:species` — delete one species. No special-case guard
  at all — deleting an assembly removes its own MySQL rows, files, JBrowse2 assembly
  (and, if this Database Version is currently default, its BLAST DB set — see §5), and
  its ES data via one uniform mechanism for all 4 assembly-scoped types: delete that
  species' concrete index for `genomiclocation`/`sequence`/`synonym`/`dsrna` (§5). Never
  touches the Database Version's shared `orthology.tsv` data, since orthology belongs to
  no single assembly (§2).

No dedicated "(re)designate target" endpoint exists — there is no target to designate.
`POST /versions/:name/release` keeps its exact current request shape (`{name}` via the
URL only, no body change) and now builds BLAST for every assembly under the version
automatically.

### Changed endpoints

- `POST /uploads` (tus) — metadata gains the required `assembly` field. **This is a
  breaking change** for any existing upload client. Unavoidable given the model change;
  no API versioning scheme exists in this repo (`CLAUDE.md` confirms no `/v1/` prefix
  anywhere), and per the "clean cutover" decision, ship it directly rather than adding
  speculative versioning infrastructure.
- `GET /versions/:name/detail` — splits today's one flat `VersionDetailFiles` bucket
  into two levels: `orthologyTSV: FileDetail[]` **stays at the top level** (Database
  Version, unchanged from today — it was already a list, never a single-file bucket) and
  every other field (`genomicFNA`, `genomicGFF`, `rnaFNA`, `cdsFNA`, `proteinFAA`,
  `dsrnaCSV`, `jbrowseTrack`, `speciesSynonym`) moves into a new `assemblies:
  {assemblyId, name, species, status, files}[]` array, one entry per Assembly Version.
  The Database Version's own `isDefault` field keeps its exact current meaning (compares
  against `app_settings.default_version_id`, unchanged) — once true, every one of its
  assemblies is BLASTable, with no further per-assembly distinction needed.
- `POST /versions/:name/release` — **request/response shape unchanged** (no new
  required field). Internally now validates every Assembly Version has its required
  files (§3) and builds BLAST DBs for all of them; `ReleaseResult` gains per-assembly job
  breakdowns; can now fail with `ErrRequiredFileNotUploaded` (per-assembly, in addition
  to its existing meaning) if any assembly is missing `genomic.fna`.
- `GET /upload-files?version=` — gains an optional `assembly=` filter param (the
  assembly's `species` code, same convention as everywhere else); `UploadFileSummary`
  gains `assemblyVersionId`/`species` fields for reference.
- `GET /silencingseqs` — **explicitly out of scope for this design, deferred by the
  user.** Left unchanged for now (still gated by the global `mainSpecies == "Tcas"`
  check, §1); multi-species behavior for this endpoint will be specified in a future
  requirements update rather than designed here. Note for whenever that happens: dsRNA
  data is not merged behind a shared alias the way orthology/synonym/sequence/genomic
  are (§9), so resolving "which assembly" will need its own explicit param at that time.

### Endpoints needing NO change (confirmed, not assumed)

- `/orthology/:species`, `/genes/:species` — already accept arbitrary species strings as
  literal ID-prefix filters against documents that already carry `Species:GeneID`
  shapes. `genes.go` reads `synonym`/`sequence`/`genomiclocation` — all 3 index-split
  by species now, reachable transparently via their shared aliases (§5); `orthology.go`
  reads the always-shared orthology index. Either way, both endpoints see every species
  with no handler-level awareness that "assembly" exists at all.
- `/public/versions` — only exposes `{id, name, createdAt}`, untouched.
- `/search`, `/search/_suggest` — read `synonym` (now index-split by species, reachable
  via its shared alias, §5) and `orthology` (always shared). Both fan out correctly
  under their respective aliases with no handler changes needed. The "main species
  first" sort (§1) is the one internal
  detail that changes: with no more process-wide `config.MainSpecies` and no
  per-assembly "default" concept (species are fully symmetric now, §2), there is no
  longer a principled "which one is main" — `enrichOrthology`'s sort
  (`search.go:184-191`) simplifies to a plain deterministic ordering (alphabetical by
  species, the simplest stable choice) instead of a main-species-first one. This is a
  genuine, minor UX behavior change worth calling out to whatever external client
  renders `/search` results, but the endpoint's request/response shape is unchanged.

### Example payloads

```
POST /versions/v2/assemblies
{ "name": "Human / GRCh38", "species": "Hsap" }
→ 201 { "id": 7, "versionId": 3, "name": "Human / GRCh38", "species": "Hsap", "createdAt": "..." }

POST /versions/v2/assemblies
{ "name": "Mouse / GRCm39", "species": "Mmus" }
→ 201 { "id": 8, "versionId": 3, "name": "Mouse / GRCm39", "species": "Mmus", "createdAt": "..." }

GET /versions/v2/detail   (before any release; one shared orthology.tsv uploaded)
→ {
    "id": 3, "name": "v2", "isDefault": false,
    "status": "READY",
    "orthologyTSV": [ { "id": "...", "filePath": "...", "uploadStatus": "COMPLETED", "jobs": [...] } ],
    "assemblies": [
      { "assemblyId": 7, "name": "Human / GRCh38", "species": "Hsap", "status": "READY", "files": { "genomic.fna": {...}, ... } },
      { "assemblyId": 8, "name": "Mouse / GRCm39", "species": "Mmus", "status": "DRAFT", "files": { "genomic.fna": {...}, ... } }
    ]
  }
  // orthologyTSV is shared — it references genes from BOTH assemblies, not owned by either

POST /versions/v2/release
(no body)
→ 200 { "id": 3, "name": "v2", "jobs": [ /* SETUP_BLAST jobs for BOTH assemblies' genomic.fna */ ... ] }

GET /versions/v2/detail   (after release; app_settings.default_version_id = 3)
→ {
    "id": 3, "name": "v2", "isDefault": true,
    "status": "READY",
    "assemblies": [
      { "assemblyId": 7, "name": "Human / GRCh38", "species": "Hsap", "status": "READY", "files": { "genomic.fna": {...}, ... } },
      { "assemblyId": 8, "name": "Mouse / GRCm39", "species": "Mmus", "status": "DRAFT", "files": { "genomic.fna": {...}, ... } }
    ]
  }
  // both Hsap and Mmus are now simultaneously BLASTable — see §5
```

### Backward compatibility posture

No versioning scheme is introduced — this is a direct breaking change to `/uploads` and
`/versions/:name/detail`, acceptable under the confirmed clean-cutover decision.
`/orthology/:species`, `/genes/:species`, `/public/versions`, `/search` remain
byte-compatible.

---

## 5. Database Migration & Storage Changes

### Migrations

- `migrations/000005_create_assembly_versions.up/down.sql` — the new, fully symmetric
  table (§2).
- `migrations/000006_alter_jobs_upload_files_assembly_version.up/down.sql` — separate
  migration (must run after 000005, since it FKs into `assembly_versions`): rename
  `upload_files.version_id` / `jobs.version_id` to `assembly_version_id`, repoint the FK.

That's the complete migration set — `app_settings` and `versions` need no schema change
at all (§2).

Given the confirmed **clean-cutover** decision (no production data to preserve), the
`up` migration does not need a data-backfill step — existing dev/staging rows can simply
be dropped and recreated against the new schema. If any dev/staging data is worth a few
extra minutes to keep, an optional one-off script (create exactly one Assembly Version
per existing Database Version, named from the deployment's current `MAIN_SPECIES` env
value, then repoint existing `upload_files`/`jobs` rows to it) is straightforward to add
later — not proposed as part of the required migration path given the confirmed
decision.

### Filesystem storage

`{uploadDir}/{versionName}/{fileName}` → `{uploadDir}/{versionName}/{species}/
{fileName}` for every type except `orthology.tsv`, which keeps its current flat path
(§3). `DeleteVersion`'s existing `os.RemoveAll(filepath.Join(uploadDir, v.Name))`
(`version.go:552-555`) needs **no change** — it already recursively removes everything
under the version directory, which now includes both the per-species subdirectories and
the unchanged flat orthology file. A new "delete one assembly" path needs the narrower
`os.RemoveAll(filepath.Join(uploadDir, v.Name, assembly.Species))` — this naturally
never touches orthology's file, which lives one level up.

### Elasticsearch — species goes into the index name for all 4 non-shared types; no `species` field needed

**Second correction, from the same re-verification exercise**: the previous draft split
`genomiclocation`/`dsrna` by species but kept `sequence`/`synonym` sharing one index,
requiring a `species` *field* on documents as the only way to tell species apart within
that shared index. Since searching across many same-schema indices is a normal,
well-supported ES pattern (established earlier in this discussion), there's no reason to
accept that asymmetry — put `species` in the index name for **all 4** non-orthology
types, uniformly. This is both more consistent and less code: no new entity fields, no
new ES templates, no delete-by-query path, and no per-type branching in "delete one
assembly."

**The mechanism differs by *why* each type currently shares an index, verified against
each handler (`internal/pkg/usecase/worker/handlers/*.go`):**

- `genomiclocation`/`dsrna` use `time.Now().Unix()` — a **genuinely fresh index on every
  upload**, intentionally: re-uploading a GFF means "replace the whole gene set with
  this corrected one," not "add more genes on top." This is also exactly where the real
  write-collision bug lives — `SetAlias`/`DeleteStaleIndexes` wipe out *every* other
  index under the alias, which is fine when there's only ever one species, but destroys
  sibling species' data once there's more than one. **Fix: keep this exact mechanism,
  just scope it per species** — `{prefix}-{type}-{versionSlug}-{species}-{ts}`, and scope
  `SetAlias`/`DeleteStaleIndexes` to only touch index(es) matching that species'
  prefix, leaving siblings alone (unchanged from the previous draft for these two types).
- `sequence`/`synonym` use `version.CreatedAt.Unix()` — a **fixed value per grouping
  key**, deliberately, so that multiple *files* accumulate into one index rather than
  displacing each other (RNA+CDS+protein for `sequence`; multiple synonym files for
  `synonym` — both have explicit code comments confirming this was a historical fix for
  exactly this displacement problem, just not a species-related one). **Fix: extend the
  grouping key from `(version)` to `(version, species)`, keeping the same fixed-timestamp
  trick** — `{prefix}-{type}-{versionSlug}-{species}-{ts}` where `ts` is still
  `version.CreatedAt.Unix()`, not `time.Now()`. Re-uploading RNA then CDS then protein
  for the *same* species still computes the identical index name every time (safe,
  accumulates as today) — only a *different* species produces a different index name.
  No `DeleteStaleIndexes` needed for these two, same as today.
- `orthology` is the only type that stays fully shared, unscoped by species — it's
  Database-Version-wide by nature, not owned by any one assembly (§2), so there's no
  species dimension to add to its index name at all.

**Keep the alias version-scoped for all 4** (`{prefix}-{type}-{versionSlug}`,
byte-identical to today) — only the concrete index name gains the `species` segment.
Querying via the alias still fans out across every species' concrete index for free,
which is what keeps `/search`, `/orthology/:species`, `/genes/:species` working with no
handler-level awareness that "assembly" exists (below).

This makes "delete one assembly" **uniform across all 4 types** — always a plain
`esindex.DeleteIndexesByAssemblyVersion(ctx, versionName, species)` (GET-then-DELETE on
`{prefix}-{type}-{versionSlug}-{species}-*`, same shape as `DeleteIndexesByVersion`),
never a delete-by-query. `orthology` needs neither — deleting an assembly never touches
it at all (§2).

**No entity or ES template changes needed anywhere** — `entity.GenomicLocation`,
`entity.Synonym`, `entity.DsRNA` don't need a new `Species` field, since the index name
now fully identifies species for these types; `cmd/esmigrate/esmigrate.go`'s templates
are untouched. `sequence`'s pre-existing `species: keyword` field (already in production
before any of this work) is left exactly as it is — no reason to remove something that
already exists and works — it's just no longer load-bearing for this design.

**One trade-off worth naming plainly, not glossing over**: since `synonym` is
index-split again, the per-shard relevance-scoring caveat discussed earlier re-applies
to its ranked/fuzzy queries (`/search/_suggest`, `FindBySynonymRelaxed`) — splitting by
species means each shard's term statistics are computed within one species' corpus
rather than a cross-section of the whole dataset, which can skew which result "wins"
when scores from differently-sized species' indices merge. This doesn't affect
`genomiclocation`/`dsrna`/`sequence`'s exact-match/ID-based access patterns at all — no
ranking involved there. It's mitigable per-query via `search_type=dfs_query_then_fetch`
(global term stats across every shard, at the cost of one extra round-trip) if it ever
proves to matter in practice; not a reason to avoid this design, just a known,
narrow, already-understood characteristic of it.

### JBrowse2 — opaque IDs, not a `species`-code composite

`$VERSION` today plays three roles at once in the scripts: (a) the internal assembly
`.name` used for `jq` equality matching, (b) the on-disk filename prefix, (c) the
human-facing track title (`"${VERSION} Annotations"`). A composite built from the
Database Version name and the assembly's `species` code is a poor fit for all three
simultaneously — fragile in shell/glob/jq-equality contexts, not guaranteed unique
across different Database Versions (two versions could both have an `"Hsap"` assembly),
and specifically dangerous for "delete every assembly belonging to this Database
Version," which today works via a single `jq` equality/prefix check that a
species-code-based delimiter can't safely guarantee.

**Use an opaque, DB-ID-derived identifier for the `.name`/filename role, and carry the
assembly's `name` field as a separate human-readable label for the title role**:

- `AssemblyID := fmt.Sprintf("v%da%d", version.ID, assemblyVersion.ID)` — pure digits
  plus two fixed letters, immutable even if `name`/`species` are later edited, safe in
  every shell/jq/filename context, trivially unique with no derivation involved.
- Add one new script parameter, `DISPLAY_LABEL` (e.g. `"{versionName} — {assembly.name}"`,
  using the assembly's human-readable `name` field, not its short `species` code), used
  only where scripts currently interpolate `$VERSION` into a *title* string
  (`add-track --name "${VERSION} Annotations"` → `"${DISPLAY_LABEL} Annotations"`).
  Every other call site keeps its exact current logic, just fed `$ASSEMBLY_ID` instead
  of `$VERSION` — a small, mechanical script diff, not a rewrite.
- **"Delete whole Database Version"**: rather than teaching `delete_jbrowse_version.sh`'s
  `jq` a new prefix-match rule, enumerate that Database Version's `assembly_versions`
  rows in Go (needed anyway for the MySQL/ES cascade) and invoke the *existing
  single-assembly delete script* once per assembly. Reuses one code path for both "delete
  one assembly" and "delete a whole Database Version," and avoids introducing any new
  jq prefix-matching logic at all.
- `set_default_jbrowse2_view.sh` — since every assembly is now symmetric (no
  "default species" concept exists anywhere, per §2), there is no single assembly to
  promote. Simplify its job to: when a Database Version is promoted to default, move
  **all** of its assemblies' views to the front of `defaultSession.views` (ordered
  deterministically, e.g. by `AssemblyID`) rather than one designated view. Opening
  JBrowse2 with no query params still lands on whichever view is literally first in the
  resulting array — a minor, purely cosmetic detail, not a data-visibility one (every
  assembly's data is already loaded and browsable regardless of view order, per the
  existing "JBrowse2 assembly/track creation happens at upload time, independent of
  release status" behavior in `CLAUDE.md`).
- Every Assembly Version gets its own JBrowse2 assembly + tracks, unconditionally, with
  no special-casing of any one assembly at all, anywhere.

### BLAST — becomes per-Assembly-Version; script bodies stay generic, only call-site paths change

**This is a revised, final decision** — an earlier draft of this design kept BLAST as a
single global target (3 fixed paths, one admin-chosen species at a time). The user
corrected this: every Assembly Version under the default Database Version must be
simultaneously BLASTable, with no target/primary concept anywhere (§2). This changes
more than the earlier draft assumed:

- **Path scheme: flat filenames, not nested subdirectories** — the 3 fixed global paths
  (`blastDBPath+"/genome"`, `/protein`, `/rna"`, hardcoded in `cmd/worker/worker.go:92-106`
  today) become one flat filename per (assembly, type) triplet, still directly under
  `blastDBPath`: `{blastDBPath}/{AssemblyID}-genome`, `{blastDBPath}/{AssemblyID}-protein`,
  `{blastDBPath}/{AssemblyID}-rna` — reusing the same opaque `AssemblyID`
  (`v{versionID}a{assemblyID}`) introduced for JBrowse2 above, for the same
  shell-safety/immutability/collision-safety reasons (two assemblies in different
  Database Versions could otherwise share the same `species` code). **Deliberately
  flat, not nested**: SequenceServer already reliably discovers multiple sibling BLAST
  DB file sets in one flat directory today (that's exactly what the existing 3 fixed
  names are) — going from 3 flat names to `3 × N` flat names is the same proven
  mechanism, just more of it, with no dependency on whether SequenceServer recursively
  scans subdirectories (an earlier draft assumed nested per-assembly folders and flagged
  that scanning behavior as an unconfirmed risk; this flat scheme removes the need to
  verify it at all).
- **Human-readable, per-species names in the SequenceServer picker come from `-title`,
  not the filename** — confirming the user's requirement ("distinct name for each
  species, e.g. `human-protein`, `human-mrna`"): the `-out` filename above only needs to
  be unique and stable, so it stays opaque/ID-based; the **display** name is controlled
  entirely by `setup_blast.sh`'s existing `-title` argument (`TITLE="$3"`,
  `makeblastdb ... -title "$TITLE"`, unchanged). The Go call site builds this per
  assembly from its human-readable `name` field as `"{blastTitle} {assembly.name}
  {typeLabel}"` (e.g. `"EMOBase Human / GRCh38 Protein"`, `"EMOBase Mouse / GRCm39
  Genome"`) instead of today's fixed `blastTitle+" Genome"`/`" Proteins"`/`" RNAs"` —
  this is what SequenceServer shows in its database picker, exactly matching the
  "distinct name for each species" requirement.
- **`setup_blast.sh` / `remove_blast.sh` themselves need zero changes** — both are
  already fully generic, taking `$OUT` as a caller-supplied argument
  (`makeblastdb ... -out "$OUT"`, `rm -f "${OUT}".*`). Only the **Go-side call site**
  changes: instead of 3 fixed constants passed to `handlers.NewSetupBlastHandler` at
  worker startup (`cmd/worker/worker.go:92-106`), the output path must now be
  constructed per-assembly at job-dispatch time from the job's own `AssemblyID` — this
  moves from "baked into the handler at wiring time" to "computed per job," a real but
  contained code change.
- **`ReleaseVersion`'s blastSpec loop runs once per Assembly Version** (§3) — for each
  assembly: required `genomic.fna` → `SETUP_BLAST`; optional `protein.faa`/`rna.fna` →
  `SETUP_BLAST` if present. `SetupBlastPayload` gains the assembly's `AssemblyID`.
- **`REMOVE_BLAST` becomes effectively dead code, worth removing rather than carrying
  forward** — its entire purpose today is cleaning up a stale DB left at a *shared
  global path* by a file type this version doesn't have (`version.go:394-399`'s
  comment: "if it was ever built... the on-disk database would otherwise be left
  stale"). Once paths are per-(version, assembly) rather than 3 shared slots, that
  collision can't happen — within one Assembly Version, `protein.faa`/`rna.fna`
  presence is monotonic (there's no delete path for those file types, per
  `deletableFileTypes`, §1), so "had one, now doesn't" never occurs post-migration. Flag
  this as a recommended cleanup (drop `JobTypeProteinFAARemoveBlast`,
  `JobTypeRNAFNARemoveBlast`, `RemoveBlastHandler`, `RemoveBlastPayload`) rather than a
  requirement — Phase 4 can carry it forward unused with no correctness cost if the team
  prefers a smaller diff.
- **Switching the *default Database Version* still needs an explicit cleanup step that
  didn't exist before**: today, overwriting the 3 fixed global paths on every release
  automatically discards the previous version's BLAST data for free. With per-assembly
  filenames, a new default version's assemblies land at *different* filenames than the
  old default's — so `finalizeBlastRelease` (`usecase/worker/handlers/blast_shared.go`)
  gains a new step: read the *previous* `app_settings.default_version_id` before
  overwriting it, and if it differs from the version just finished, remove every file
  matching `{blastDBPath}/v{oldVersionID}a*` (a plain `filepath.Glob` + `os.Remove` per
  match in Go, no new shell script needed, reusing the same version-ID-prefix-matching
  trick `esindex.DeleteIndexesByVersion` already uses for ES — simpler here even, since
  these are flat files rather than directories, no recursive removal needed). Without
  this step, SequenceServer would keep showing a previous version's species alongside
  the current default's — a real correctness requirement, not just disk hygiene.
- **`finalizeBlastRelease`'s completion check needs no change at all** —
  `HasNonDoneJobOfTypesForVersion(versionID, blastJobTypes)` already filters on
  `jobs.version_id`, which (per §2's additive schema) every per-assembly `SETUP_BLAST`
  job still carries directly alongside its new `assembly_version_id`. So this existing
  query, unmodified, already means exactly "are all BLAST-related jobs done for *every*
  assembly under this Database Version" — no join needed, a pleasant consequence of
  keeping `version_id` on every row rather than replacing it.

---

## 6. Backward Compatibility

- `entity.Version`, `app_settings` (table and repository methods), all 5 ES index
  templates (§5 — no document-level `species` field needed anywhere), `setup_blast.sh`/
  `remove_blast.sh` (script bodies), `esindex.DeleteIndexesByVersion`'s pattern shape,
  `/orthology/:species`, `/genes/:species`, `/public/versions` — all fully
  unchanged (§2, §4, §5).
- `/uploads` and `GET /versions/:name/detail` are **breaking changes** — no way around
  them given the model change, and no versioning scheme exists to soften them. Ship
  directly per the confirmed clean-cutover decision rather than adding speculative
  `/v1/` infrastructure for a single breaking migration.
- `config.MainSpecies` should be **removed outright, not deprecated** — every one of its
  6 consumption sites (§1) maps cleanly to a per-assembly replacement, and the user
  confirmed no production-compatibility burden exists. Its required-non-empty startup
  check (`config.go:108-110`) is deleted along with the field.
- One coordination risk outside this repo's control: confirmed this repo has **no**
  upload-client source code (backend-only), so any script/tool that calls `/uploads`
  today lives elsewhere and will need the new `assembly` metadata field added in
  lockstep — flagged in §10, not something this design can resolve unilaterally.

---

## 7. Frontend / User Workflow

No frontend/UI source exists in this repository (confirmed: only `cmd/`, `internal/`,
`migrations/`, `scripts/`, `docs/`, `nginx/`, `data/`; `data/jbrowse2/` is the
pre-built official JBrowse2 static bundle, not custom source). Any admin/consumer UI for
this API lives in a separate repository, out of scope for this design.

For that external UI's workflow to be well-specified, though, the intended admin flow
under the new model is:

1. **Create a Database Version** — unchanged: `POST /versions {name}`.
2. **Add an Assembly Version / species** — new: `POST /versions/:name/assemblies
   {name, species}`. Every assembly is created equal — none is auto-marked special.
3. **Upload files** — unchanged tus flow, plus the new required `assembly` metadata
   field selecting which Assembly Version each upload belongs to.
4. **Validate an Assembly Version** — `GET /versions/:name/assemblies/:species` shows its
   own status (`READY`/`DRAFT`/`MISSING_REQUIRED_FILE`/etc.), same semantics as today's
   whole-version status, now scoped per species.
5. **Add multiple species to the same Database Version** — repeat steps 2-4 per species;
   `GET /versions/:name/detail` shows the Database-Version-level rollup plus a
   per-assembly breakdown.
6. **Release** — `POST /versions/:name/release` (unchanged request shape) now validates
   every assembly has its required files, then builds BLAST DBs for **all** of them —
   every species in the version becomes simultaneously BLASTable once release completes,
   with no per-species selection step at all.

---

## 8. Testing Strategy

No `_test.go` files exist in this repo today (confirmed via `CLAUDE.md` and direct
listing) — this design doc should flag explicitly whether this migration is also meant
to introduce test coverage, since "preserve all existing per-file-type validation
rules" is hard to *prove* preserved without one (§10).

**Unit**
- `species`-code uniqueness rejection: creating a second assembly with an identical
  `species` value under the same Database Version → 409 (`UNIQUE(version_id, species)`).
- Per-assembly BLAST path construction (`{blastDBPath}/{AssemblyID}-{genome,protein,
  rna}`) produces distinct, collision-free flat filenames for every assembly.
- Per-assembly required-file check; Database-Version rollup status against the exact
  rule in §3 (including the `n == 0` and `n == 1` degenerate cases).
- GFF → SPECIES.SYNONYM species resolution now uses the assembly's own `Species`, not a
  global constant.
- `sequence`/`synonym`'s extended index-name grouping key: uploading RNA, then CDS, then
  protein for the *same* assembly computes the identical index name each time (still
  accumulates safely, unchanged from today); a *different* assembly's upload computes a
  different index name.

**Integration**
- Full upload → job → ES pipeline for 2 species within 1 Database Version, across all 4
  assembly-scoped types: assert each species lands in its own concrete index, all
  reachable via their shared alias.
- **Index-collision regression test, `genomiclocation`/`dsrna`** (direct regression test
  for their `time.Now()`-per-upload mechanism, §5): upload `genomic.gff` for assembly A,
  then for assembly B, in the same Database Version; assert re-uploading B's GFF a
  second time does not delete A's concrete index via `SetAlias`/`DeleteStaleIndexes`.
- **Accumulate-vs-isolate test, `sequence`/`synonym`**: uploading RNA+CDS+protein (or
  multiple synonym files) for assembly A all land in A's one index (accumulate,
  unchanged behavior); assembly B's uploads land in a completely separate index
  (isolate, the new behavior) — both properties verified together, since the fix reuses
  one mechanism for both.
- **Cross-species search fan-out**: with 2 species' `synonym` data split across 2
  indices under one alias, `/search`/`/search/_suggest` still return matches from
  either species with no code path needing to know "assembly" exists — this is the
  regression test that would have caught the original per-type conflation.
- JBrowse2 whole-Database-Version delete loops correctly over N assemblies without
  leaving orphaned `.name`-keyed views/tracks (regression test for the §5
  loop-over-single-assembly-delete design).
- **Version-switch BLAST cleanup regression test** (direct regression test for the §5
  `finalizeBlastRelease` addition): release Database Version A (2 assemblies), release
  Database Version B (different assemblies), assert every file matching
  `{blastDBPath}/v{A}a*` is gone and only B's assemblies remain servable.

**API**
- New assembly CRUD endpoints; `/uploads` metadata validation (missing `assembly` → 400;
  `assembly` belonging to a different Database Version → 400/404).
- Release validation: partially-incomplete assembly blocks release; zero-assembly
  Database Version blocks release (`ErrRequiredFileNotUploaded` in both cases, §3).
- Deleting an assembly never removes or touches the Database Version's shared
  `orthology.tsv` data or index, even when the deleted assembly's species appears in
  existing ortholog groups (§2, §5) — the orthology-owning-assembly guard from an
  earlier draft is gone because there's no longer an "owning" assembly to guard.

**Migration**
- Migrations `000005`/`000006` apply cleanly against a fresh schema and in order
  (`000006` depends on `000005`'s table existing).

**E2E**
- Two-species Database Version, full lifecycle: create version → create 2 assemblies →
  upload genomic.fna+gff (+ optional protein/RNA) for both → release (no per-species
  selection) → verify **both** species are simultaneously BLASTable in SequenceServer →
  verify JBrowse2 shows both assemblies simultaneously → verify `/genes/:species` and
  `/orthology/:species` return correct, non-cross-contaminated results for both species
  → release a *different* Database Version → verify the first version's BLAST data is
  now gone from SequenceServer and only the newly-released version's assemblies remain.

---

## 9. Implementation Plan

Phase 0 blocks everything. Phases 1 and 2 can start once Phase 0 lands (Phase 2 depends
on Phase 1's assembly-resolution logic existing). Phases 3 and 4 both depend on Phase 2
(need `assembly_version_id` on jobs/files) but are independent of each other and can run
in parallel. Phase 5 runs last, since it removes the `MainSpecies` fallback entirely.

**Phase 0 — Schema, entity, repository layer**
- `migrations/000005_create_assembly_versions.up/down.sql` (the symmetric table, §2)
- `migrations/000006_add_assembly_version_id.up/down.sql` (additive: adds nullable
  `assembly_version_id` to `upload_files`/`jobs`; existing `version_id` on both is
  untouched; must run after 000005 since it FKs into `assembly_versions`)
- `internal/pkg/entity/assembly_version.go` (new);
  `internal/pkg/entity/job.go`, `entity/upload_file.go` (add `AssemblyVersionID
  *uint64`, keep existing `VersionID uint64` unchanged); `entity/version.go` is
  **unchanged**
- `internal/pkg/repository/assemblyversion/` (new, mirrors `repository/version/mysql.go`);
  `repository/job/mysql.go`, `repository/uploadfile/mysql.go` — all existing
  `*ByVersionID` methods are **unchanged** (still query the untouched `version_id`
  column); add new `*ByAssemblyVersionID` variants alongside them for the per-species
  queries introduced by this design; `repository/appsettings/mysql.go` is **unchanged**

**Phase 1 — Assembly Version CRUD, status rollup, API/OpenAPI**
- `internal/pkg/usecase/assemblyversion/` (new: create — no special-casing, every
  assembly is created identically; delete has no orthology-related guard at all, §2/§5)
- `internal/pkg/usecase/version/version.go` (status rollup, `ReleaseVersion` — same
  signature as today, its blastSpec loop now iterates every Assembly Version,
  `GetVersionDetail`, `DeleteVersion` cascading through `assembly_versions` — its
  existing `ErrCannotDeleteDefaultVersion` guard needs **no change** at all, since
  `app_settings`/`GetDefaultVersionID` are untouched)
- `internal/pkg/api/handler/assemblyversion.go` (new), `api/router.go`,
  `docs/openapi.yaml`

**Phase 2 — Upload pipeline re-scoping** (depends on 0+1)
- `internal/pkg/usecase/upload/upload.go` (§3: metadata field, path construction, dedup
  scoping, GFF→SYNONYM species arg, FNA lookup scoping, dsrna gate)
- `internal/pkg/jobpayload/*.go` (add the opaque `AssemblyID` where payloads currently
  only carry `VersionID`/`VersionName`: `SetupJBrowse2FNAPayload`,
  `SetupJBrowse2GFFPayload`, `JBrowseTrackPayload`, `SetupBlastPayload`; consider
  dropping `RemoveBlastPayload`/the `*REMOVE_BLAST` job types entirely per §5's
  dead-code observation)

**Phase 3 — ES index/alias split, search** (depends on 2; independent of 4)
- `internal/pkg/repository/{genomic,dsrna}/es.go` (assembly-scoped `SetAlias`/
  `DeleteStaleIndexes`, unchanged mechanism from the previous draft — these 2 already
  create a genuinely fresh index per upload, so the fix is scoping that existing
  mechanism to species, §5)
- `internal/pkg/usecase/worker/handlers/{sequence_fasta,synonym}.go` (extend the
  existing fixed-per-grouping-key index name from `(version)` to `(version, species)` —
  same `version.CreatedAt.Unix()` trick already in place, just narrowed; no `SetAlias`/
  `DeleteStaleIndexes` signature change needed for these two, since they never had
  `DeleteStaleIndexes` calls to begin with, §5)
- `internal/pkg/usecase/worker/handlers/{genomic_gff,dsrna_csv}.go` (2 files —
  `species`-code segment on the existing `time.Now()`-based concrete index name) —
  `orthology_tsv.go` is **unchanged**, no species dimension at all (§2)
- **No entity or ES template changes** — `entity.GenomicLocation`/`Synonym`/`DsRNA`
  need no new field, `cmd/esmigrate/esmigrate.go` is fully untouched (§5)
- `internal/pkg/repository/esindex/es.go` (new `DeleteIndexesByAssemblyVersion`,
  covering all 4 assembly-scoped types uniformly — plain index deletion, no
  delete-by-query path needed anywhere)
- `internal/pkg/usecase/search/search.go` (`enrichOrthology`'s "main species first" sort
  simplifies to a plain deterministic ordering, §4), `genes.go`, `orthology.go`
  (confirmed likely unchanged given the alias fix — write the regression test from §8 to
  prove it). `GetSilencingSeqs`/`/silencingseqs` is **out of scope for this phase** —
  deferred by the user (§4); left exactly as-is.

**Phase 4 — JBrowse2/BLAST re-scoping** (depends on 2; independent of 3)
- `scripts/{setup_jbrowse2_fna,setup_jbrowse2_gff,add_jbrowse_track,
  delete_jbrowse_version,set_default_jbrowse2_view}.sh` (opaque `ASSEMBLY_ID` +
  `DISPLAY_LABEL` params, §5); `setup_blast.sh`/`remove_blast.sh` need **no** script
  changes, only their Go call sites do
- `internal/pkg/usecase/worker/handlers/{setup_jbrowse2,setup_blast,blast_shared}.go`
  (per-assembly `$OUT` path construction, `finalizeBlastRelease`'s
  all-assemblies-done check and new previous-default-version cleanup step, §5),
  `usecase/version/version.go` (`ReleaseVersion`'s per-assembly blastSpec loop),
  `cmd/worker/worker.go` (BLAST handler wiring moves from 3 fixed constants to
  per-job-computed paths)

**Phase 5 — `config.MainSpecies` removal, docs cleanup** (depends on all above)
- `internal/pkg/config/config.go`, `cmd/api/api.go`, `cmd/worker/worker.go`,
  `docs/openapi.yaml`, `CLAUDE.md`

### Rollout

Given the confirmed clean-cutover decision, no phased dual-write/backfill rollout is
required — deploy schema + code together per environment, re-upload data as needed. The
main sequencing risk is entirely intra-repo (phase dependencies above), not
data-migration risk.

---

## 10. Risks / Open Questions

1. **Orthology's cross-species nature is fully resolved, not a risk** — the user
   confirmed orthology.tsv is shared across every species in a Database Version rather
   than owned by one assembly. This eliminated an entire earlier open question (the
   delete-ownership guard, §5) rather than just answering it: there is no longer any
   tension to track here.
2. **The Tcas-only dsrna gate is confirmed to stay, for now** — the user explicitly
   confirmed this is a real, currently-needed product constraint (re-scoped from global
   to per-assembly in §3, not removed). Its eventual removal is a decision the product
   owner will make separately in the future; not proposed or assumed by this migration.
3. **`MISSING_REQUIRED_FILE` reuse for the zero-assembly rollup case** (§3) needs
   coordination with whatever external client consumes `VersionItem.Status`/
   `VersionDetail.Status` today — flagged, not assumed resolved, since this repo has no
   visibility into that client.
4. **Upload-client coordination** — this repo is backend-only; any script/tool that
   calls `/uploads` today lives elsewhere and needs the new required `assembly`
   metadata field added in lockstep. Worth confirming what currently drives uploads
   before shipping Phase 2.
5. **No test suite exists today** (§8) — "preserve existing validation rules" is hard to
   *prove*, not just assert, without one; worth deciding whether this migration is also
   the point tests get introduced.
6. **Pre-existing, not introduced by this design**: `indexname.FromVersionName` is lossy
   (case-insensitive, collapses distinct punctuation to `_`), and `versions.name` has no
   slug-uniqueness constraint of its own — two Database Version names could already
   collide on ES index prefix today, independent of assemblies. Worth a follow-up ticket
   regardless of this migration; it compounds with the new per-assembly `species`
   dimension added to the same index names.
7. **SequenceServer multi-database discovery is resolved, not a risk** — the earlier
   draft's design used nested per-assembly subdirectories and flagged SequenceServer's
   recursive-scan behavior as unconfirmed. The user's clarification (flat, distinctly
   named files per species — "human-protein," "human-mrna," etc.) led to a flat-filename
   scheme instead (§5): `3 × N` flat files in the same directory, the same proven
   mechanism as today's 3 fixed files, just more of them. Nothing left to verify here.
8. **Version-switch BLAST cleanup has a small timing window** (§5): `finalizeBlastRelease`
   removes the previous default version's stale BLAST files *after* the new version's
   jobs all complete but as a separate step from the Docker container restart — a request
   arriving in between could theoretically see a container that's been restarted but
   still has stale files on disk, or vice versa, for a brief window. Today's code has a
   similar (accepted) eventual-consistency character already (per the existing
   `HasInFlightJobOfType`/N+1 TODOs in `version.go`); flagging this as the same class of
   accepted tradeoff, not a new category of risk.
9. **Accepted, low-priority trade-off**: `synonym` being index-split by species (§5)
   means its ranked/fuzzy queries (`/search/_suggest`, `FindBySynonymRelaxed`) compute
   relevance per-shard, within one species' corpus rather than a representative
   cross-section of the whole dataset — this can occasionally let a less-relevant match
   from a small species' index outrank a better one from a larger species' index, purely
   because term rarity is judged against a smaller denominator. Mitigable per-query via
   `search_type=dfs_query_then_fetch` (global term stats, one extra round-trip) if it
   ever proves to matter in practice; not something to pre-emptively build against.

---

## Critical Files Reference

- `internal/pkg/usecase/upload/upload.go` — upload pipeline, the GFF→SYNONYM cross-job
  logic, path construction (§3)
- `internal/pkg/usecase/version/version.go` — release validation, status rollup, delete
  cascade (§3, §5)
- `internal/pkg/repository/genomic/es.go` — representative of `genomic`/`dsrna`'s
  `SetAlias`/`DeleteStaleIndexes`, scoped to species (§5); `usecase/worker/handlers/
  sequence_fasta.go` and `synonym.go` — representative of the narrower fixed-index-name
  grouping-key extension for the other 2 assembly-scoped types (§5)
- `scripts/delete_jbrowse_version.sh` — representative of the 5 JBrowse2 scripts needing
  the `ASSEMBLY_ID`/`DISPLAY_LABEL` split (§5)
- `internal/pkg/usecase/search/search.go` — the 6 `mainSpecies` consumption sites (§1);
  note `/silencingseqs` itself is deferred, out of scope (§4)
- `cmd/worker/worker.go` — job handler wiring, the 3 global BLAST paths (§1)
- `migrations/000001_create_versions.up.sql` — schema pattern reference for the new
  `assembly_versions` migration (§9)

## Verification

This is a design document — there is no code to run. To validate the design itself
before implementation begins:
1. Confirm the `assembly` upload-client coordination question (§10, risk 4) with
   whoever owns the tooling that calls `/uploads` today.
2. Once Phase 0-2 land, the alias fan-out regression test (§8) is the highest-value
   single test to write first — it's the direct proof that the ES correction in §5
   actually prevents cross-assembly data loss, which is the riskiest silent-failure mode
   in this whole design.
3. During Phase 4, confirm SequenceServer's picker correctly distinguishes all `3 × N`
   flat-named BLAST DBs by their `-title` (§5) — low risk given it's the same mechanism
   already proven by today's 3 fixed DBs, but worth a quick look the first time N > 1.
