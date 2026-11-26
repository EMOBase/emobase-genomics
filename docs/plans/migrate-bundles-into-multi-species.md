# Plan: Migrate multi-file bundles into the multi-species branch

Written for: the engineer merging `feat/multi-file-upload-bundles` into
`feat/multi-species-assembly-versions` (PR #12).

Status: Stage A (commit `2efa99f`) and Stage B are implemented and verified end to end.
D2 is resolved as tus metadata (`assembly`, checked at upload). Track children are stored
under `{version}/{species}/{bundleID}/`. Open follow-ups are listed at the end.

## Context

- **Source:** `feat/multi-file-upload-bundles`, one commit `7ac91c8` on top of `1c8e3c7`.
  It implements `orthology.bundle` and `jbrowse.track.bundle` for a single-species model. Its
  plan (`docs/plans/multi-file-upload-bundles.md`) had an end-to-end step that was never run.
- **Target:** `feat/multi-species-assembly-versions`, HEAD `7a95385`. Uploads, dedup, paths,
  and job payloads are per Assembly Version there.

## What was measured (not assumed)

1. **Read-only merge:** three textual conflicts: `docs/openapi.yaml`,
   `internal/pkg/usecase/upload/upload.go`, and a comment in `internal/pkg/jobpayload/process.go`.
2. **Trial merge** in a throwaway worktree (removed): taking our side on those files, the only
   compile errors were the stale `VersionName` field in `uploadspec` and the missing
   `ManifestColumns` / `ErrUnknownBundleType` from the conflicted `upload.go`. After patching
   those, `go vet` passed and the 12 existing bundle tests passed.
3. **Semantic gap:** `handlers/bundle.go` has no assembly awareness. It would create track
   children with no assembly, and extract them to `{version}/{bundleID}/` instead of
   `{version}/{species}/`. The merge would compile and pass its tests while writing wrong data.
4. **`uploadfile.FindByID` does not select `assembly_version_id`.** A bundle handler can't read
   its own bundle row's assembly until this is fixed. Adding it is additive; other callers of
   `FindByID` are unaffected.
5. **Release gate checks only the newest file per type.**
   `FindLatestCompletedPerTypeByAssemblyVersionID` uses `ROW_NUMBER() ... rn = 1`, and the
   version-level orthology check does the same. Older files of a type aren't awaited before
   release. This already applies to multiple single uploads. Bundles make it worse, since a
   bundle creates many children at once. **Out of scope for this migration** (see "Separate
   concern" below).

## Decisions

| # | Decision | Status |
|---|---|---|
| D1 | A bundle covers **one** scope. `orthology.bundle` is version-level, since orthology files belong to no assembly. `jbrowse.track.bundle` is scoped to **one assembly**. No mixed-assembly archives. | Confirmed |
| D2 | Unknown assembly codes fail the job, not the upload. | **Needs confirmation.** See below. |
| D3 | `trackName` must be unique per `(assembly, trackName)`. Within one bundle this is the same as unique within the bundle, since a bundle has one assembly. | Confirmed |
| D4 | `orthology.bundle` is unchanged: version-level, no `assembly`. | Implied by D1 |
| D6 | Track payloads keep the int shape: `version_id` and `assembly_version_id`. | Implied by the previous change |

### D2 needs your confirmation

With D1, a track bundle has one assembly, so the assembly can't live in the manifest per
row. The natural place is the tus metadata, the same as every other per-assembly upload:
`assembly=<species>`. The check then runs at upload time and returns 400, the same as a single
upload. That leaves the manifest unchanged from the source template, and no check runs at job
time.

If you want the assembly in the manifest instead, D2 applies as you agreed: the job fails
and lists the error. Tell me which one.

**Recommendation:** tus metadata. It keeps validation at upload time and the template
unchanged.

## Consequences of D1 (resolve the status concern)

- Bundle jobs for tracks carry an assembly, so they count in that assembly's status. They
  no longer fall into the shared orthology bucket. The earlier status concern goes away.
- Bundle children have one assembly, which is the bundle row's assembly. The handler reads it
  from the bundle row. No `assemblyVersionRepo` dependency is needed in the bundle handler.
- Both bundle types can share one code path for children: the child's assembly is the bundle
  row's assembly, which is nil for orthology.

## Stage A: orthology bundle

Stage A ships first and touches no assembly logic.

### A0: Branch
1. From `feat/multi-species-assembly-versions`, create `feat/multi-file-upload-bundles-ms`.
2. `git merge feat/multi-file-upload-bundles`. Merge rather than rebase, so the source commit
   stays intact.

### A1: Merge mechanics
3. `process.go`: keep both comment lines.
4. `upload.go` (manual). Keep our structure, and layer the source's changes on top:
   - Imports: add `uploadspec`.
   - Step 3: require `.tar.gz` for bundle types.
   - Step 4: call `uploadspec.ValidateOrthologyMeta` and `ValidateJBrowseTrackMeta`.
   - **Step 5 (about line 159):** exempt `orthology.bundle` only. `jbrowse.track.bundle`
     requires `assembly` (D2).
   - **Step 7 (about line 181):** skip assembly resolution for `orthology.bundle` only.
   - **Concurrency (about lines 204–210):** use the source's `concurrentFileTypes` map.
     Delete our version-level `HasActiveJobOfType` branch for orthology, since it's unreachable.
   - **`enqueueProcessJob`:** adopt the source's `uploadspec.NewOrthologyTSVJob` branch.
     Orthology jobs keep `AssemblyVersionID = nil`.
   - Add the source's `ManifestColumns` method and `ErrUnknownBundleType`.
   - The track branch (about line 411) is touched in Stage B.
5. `config.go`: auto-merges. Confirm both bundle types are in `allowedFileTypes` and
   `concurrentFileTypes`.
6. `uploadfile/mysql.go`: auto-merges. Apply `notBundleSQL` to `ListByVersionID`,
   `TotalFileSizeByVersionIDs`, and `FindLatestCompletedPerTypeByVersionID`.
   The `*ByAssemblyVersionID` queries don't need it, since bundle rows have a NULL assembly
   for orthology.
7. **`FindByID`:** select `assembly_version_id` and scan it into `AssemblyVersionID`.
8. `go.mod`: promote `github.com/google/uuid` to direct. Run `go mod tidy`.
9. `cmd/worker/worker.go`: auto-merges. Register the orthology bundle handler.
10. `CLAUDE.md`: auto-merges, which makes it risky. The source's intro still says "a single
    configured main species", while ours describes multi-species. Keep our top sections and
    add the bundle section.
11. `docs/openapi.yaml`: manual merge, orthology part only (see A4).

**Checkpoint:** `go build ./...`, `go vet ./...`, `gofmt -l .` are clean. Single uploads of
every type are unchanged.

### A2: Orthology bundle worker
12. `BundleHandler` for orthology: children are created with `AssemblyVersionID = nil`. Keep the
    source's extraction, validation, and idempotent child creation as they are.
    **Deviation (implemented):** children stay in the bundle's folder,
    `{version}/{bundleID}/`, instead of the flat `{version}/`. A flat folder could overwrite an
    existing single-upload file of the same name. Stage B keeps track children in
    `{version}/{species}/{bundleID}/`, which DeleteAssemblyVersion's RemoveAll already covers.
13. Cleanup removes only the staging directory, never a shared directory.

### A3: Tests
14. Keep the 12 existing bundle tests green.
15. Add tests:
    - Orthology children have nil `AssemblyVersionID` and land in `{version}/`.
    - Duplicate `order`/`algorithm` pairs are accepted, since they share a source label.
    - A bundle job rerun creates no duplicate children.

### A4: API docs
16. `openapi.yaml`: add the source's orthology bundle documentation alongside our multi-species
    docs. State that `orthology.bundle` takes no `assembly`.

### A5: End-to-end (done)
17. Orthology bundle, three files, one already gzipped: three shared children, three
    `ORTHOLOGY.TSV` jobs `DONE`, separate Elasticsearch documents per file (1 each). Deleting
    one child removed only its document (3 → 2). Bundle archive absent from the file list and
    not counted in the version size (156 bytes of children, not the 630-byte archive).
    A broken bundle (manifest names a missing file) failed with the line and file named, and
    created no children.
18. Regression: a single `orthology.tsv` upload still works with no assembly. Track bundles are
    refused at upload (400) and their template is not served.
    Not covered here: release, BLAST, and the JBrowse2 path, which are unchanged by Stage A.

## Stage B: track bundle

Starts after Stage A is verified.

### B1: Upload path
19. Step 5: `jbrowse.track.bundle` requires `assembly`, same as single tracks. Only
    `orthology.bundle` is exempt.
20. Step 7: resolve the assembly for `jbrowse.track.bundle`, so the bundle row and job carry
    `AssemblyVersionID`. The archive is then stored at `{version}/{species}/`, through the
    existing `uploadDirFor` path logic, with no change needed there.
21. The default enqueue branch already sets `AssemblyVersionID` on the bundle job, and sets
    `Species` on its `ProcessPayload`. Nothing new is needed.

### B2: Track children
22. `uploadspec.NewJBrowseTrackJob(versionID, assemblyVersionID, fileID, filePath, meta)`
    builds `JBrowseTrackPayload{VersionID, AssemblyVersionID, FilePath, TrackName, FileID,
    Category, SelectInDefaultSession}` and **sets `entity.Job.AssemblyVersionID`**. The source
    omits this, so taking it as-is silently drops single-track jobs from per-assembly status
    and from `DeleteAssemblyVersion`. The single-track path in `upload.go` passes its
    assembly.
23. `BundleHandler` for tracks: children are created with the bundle row's
    `AssemblyVersionID` and placed at `FileDir(uploadDir, version, species)`, where the
    species comes from `ProcessPayload.Species`. The manifest template stays the source's:
    `fileName,trackName,category,selectInDefaultSession`.
24. `trackName` uniqueness (D3): check within the bundle, per `(assembly, trackName)`. Since a
    bundle has one assembly, this is the same check the source already does.
25. Register the track bundle handler in `cmd/worker/worker.go`.

### B3: Tests
26. Track children get the bundle's assembly and land in `{version}/{species}/`.
27. A duplicate `trackName` within one bundle fails the job, with no children created.
28. A track bundle upload without `assembly` returns 400. An unknown `assembly` returns 400.
    No job is created.
29. A single-track upload still sets `AssemblyVersionID` on its job. This is the regression
    test for item 22.
30. A bundle rerun creates no duplicate per-assembly children.

### B4: API docs
31. `openapi.yaml`: document `assembly` as required tus metadata for `jbrowse.track.bundle`,
    and the uniqueness rule per bundle.

### B5: End-to-end
32. Track bundle with two tracks for Tcas, and a separate one for Lyst. Expect children with the
    correct `assembly_version_id`, files at `{version}/Tcas/` and `{version}/Lyst/`, and both
    tracks in the matching assembly's JBrowse2 view. Both appear under that assembly's
    `files.jbrowse.track` in version detail.
33. Delete the Lyst assembly: its track children and JBrowse2 track go away. Tcas tracks and
    all orthology children remain.
34. A bundle with a missing `assembly` is rejected at upload. No bundle directory is created.
35. Regression: every single-upload type, release, BLAST, and version detail.

Note: JBrowse2 and BLAST scripts run only inside the Docker image. Verify on the compose
stack, as the earlier phases did.

## Separate concern (not in this migration)

**Release gate checks only the newest file per type** (see Context item 5). Bundles make it
visible: a 10-track bundle leaves 9 tracks unawaited at release. Fix it in its own change:
release should await every non-deleted file's jobs, not the latest per type. Affects single
uploads too, so it deserves its own review.

## Risks

| # | Risk | Mitigation |
|---|---|---|
| R1 | Track jobs silently lose `AssemblyVersionID` if the source's `NewJBrowseTrackJob` is taken as-is. | Item 22, with the regression test in item 29. |
| R2 | Children extracted to `{version}/{bundleID}/` escape per-assembly cleanup. | Items 12–13 and 23. |
| R3 | Track bundles run before an assembly's FNA JBrowse2 setup, so `add-track` fails. | Same as single uploads today. Bundles make it likelier. Documented, not gated. |
| R4 | The source's bundle E2E never ran, so bugs will likely surface. | Stage A and B each include an E2E step. |
| R5 | Auto-merged `CLAUDE.md` and `openapi.yaml` can contradict each other silently. | Manual review in A1 and A4 / B4. |
| R6 | `FindByID` change affects other callers. | It only adds a column. Build and tests cover callers. |
| R7 | Release gate checks the newest file per type only. | Separate concern, above. |

## Execution order

A0 → A1 (checkpoint) → A2 → A3 → A4 → A5 → confirm D2 → B1 → B2 → B3 → B4 → B5.

Commit each stage separately, as was done for the multi-species phases.

## Stage B results (end to end, compose stack)

- Track bundle without `assembly`: 400 at upload. Unknown `assembly`: 400, nothing stored.
- Two track bundles, one per assembly: each child is `JBROWSE.TRACK` `DONE` on its own
  assembly. JBrowse2 has each track on the matching assembly. Files sit under
  `{version}/{species}/{bundleID}/`.
- Duplicate `trackName` in one bundle: job `FAILED` with the line and name. No children.
- Assembly delete: its track is removed from JBrowse2, its folder and rows are gone, and the
  other assembly's tracks are untouched.
- Regression: a single `jbrowse.track` upload to an assembly still works and sets its
  assembly on the job.

## Open follow-ups (not in this migration)

- A failed bundle's archive stays on disk, and its row stays as an audit record. The source
  code removes archives only on success. The same applies to a failed orthology bundle. Decide
  whether to remove the archive on terminal failure.
- A failed bundle marks its assembly `ERROR` until the bundle is removed. This is the same as a
  failed single upload, and is recorded here as expected behavior.
- Release checks only the newest file of each type (see "Separate concern").

