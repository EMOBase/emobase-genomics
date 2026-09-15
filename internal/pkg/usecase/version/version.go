package version

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"time"

	"github.com/EMOBase/emobase-genomics/internal/pkg/auth"
	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	"github.com/EMOBase/emobase-genomics/internal/pkg/jobpayload"
	"github.com/rs/zerolog/log"
)

// deleteJBrowseVersionScript removes a version's assembly, tracks, and
// text-search index entries from the shared JBrowse2 config.json, plus the
// underlying data files (see scripts/delete_jbrowse_version.sh).
const deleteJBrowseVersionScript = "/app/scripts/delete_jbrowse_version.sh"

var (
	ErrVersionAlreadyExists       = errors.New("version already exists")
	ErrVersionNotFound            = errors.New("version not found")
	ErrRequiredFileNotUploaded    = errors.New("required file not uploaded")
	ErrFileJobsNotComplete        = errors.New("file jobs not complete")
	ErrCannotDeleteDefaultVersion = errors.New("cannot delete the default version")
	ErrVersionHasActiveJobs       = errors.New("version has active jobs")
)

type VersionItem struct {
	entity.Version
	IsDefault     bool   `json:"isDefault"`
	TotalFileSize int64  `json:"totalFileSize"`
	Status        string `json:"status"`
}

type VersionList struct {
	Versions []VersionItem `json:"versions"`
	Total    int           `json:"total"`
	Page     int           `json:"page"`
	PageSize int           `json:"pageSize"`
}

// Version statuses, also used to filter /public/versions.
const (
	VersionStatusDraft               = "DRAFT"
	VersionStatusProcessing          = "PROCESSING"
	VersionStatusError               = "ERROR"
	VersionStatusReady               = "READY"
	VersionStatusMissingRequiredFile = "MISSING_REQUIRED_FILE"
)

// ValidVersionStatuses lists every valid version status; used to validate the
// optional status filter on the public versions endpoint.
var ValidVersionStatuses = []string{
	VersionStatusDraft,
	VersionStatusProcessing,
	VersionStatusError,
	VersionStatusReady,
	VersionStatusMissingRequiredFile,
}

// IsValidVersionStatus reports whether the given status is a valid filter value.
func IsValidVersionStatus(status string) bool {
	return slices.Contains(ValidVersionStatuses, status)
}

// VersionPublicItem is the limited version data returned by the public endpoint.
type VersionPublicItem struct {
	ID        uint64    `json:"id"`
	Name      string    `json:"name"`
	IsDefault bool      `json:"isDefault"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
}

func (uc *UseCase) ListVersionsPublic(ctx context.Context, status string) ([]VersionPublicItem, error) {
	versions, err := uc.versionRepo.ListPublic(ctx)
	if err != nil {
		return nil, err
	}
	defaultVersionID, err := uc.appSettingsRepo.GetDefaultVersionID(ctx)
	if err != nil {
		return nil, err
	}
	// TODO: N+1 queries as in ListVersions; status is derived from jobs and
	// upload files. Optimize (e.g. cache) if this becomes a bottleneck.
	items := make([]VersionPublicItem, 0, len(versions))
	for _, v := range versions {
		statusCounts, err := uc.jobRepo.StatusCountsByVersionID(ctx, v.ID)
		if err != nil {
			return nil, err
		}
		completedFiles, err := uc.uploadFileRepo.FindLatestCompletedPerTypeByVersionID(ctx, v.ID)
		if err != nil {
			return nil, err
		}
		hasFNA := false
		for _, f := range completedFiles {
			if f.FileType == entity.FileTypeGenomicFNA {
				hasFNA = true
				break
			}
		}
		itemStatus := computeVersionStatus(statusCounts, hasFNA)
		if status != "" && itemStatus != status {
			continue
		}
		items = append(items, VersionPublicItem{
			ID:        v.ID,
			Name:      v.Name,
			IsDefault: defaultVersionID != nil && *defaultVersionID == v.ID,
			Status:    itemStatus,
			CreatedAt: v.CreatedAt,
		})
	}
	return items, nil
}

// JobSummary is the per-file job representation inside VersionDetail.
type JobSummary struct {
	ID          uint64           `json:"id"`
	Type        string           `json:"type"`
	Description string           `json:"description"`
	Status      entity.JobStatus `json:"status"`
	Payload     *json.RawMessage `json:"payload"`
	Error       *string          `json:"error,omitempty"`
}

// FileDetail is the representation of an upload file inside VersionDetail.
type FileDetail struct {
	ID           string              `json:"id"`
	FilePath     string              `json:"filePath"`
	FileSize     int64               `json:"fileSize"`
	UploadStatus entity.UploadStatus `json:"uploadStatus"`
	CreatedAt    time.Time           `json:"createdAt"`
	CreatedBy    string              `json:"createdBy"`
	CompletedAt  *time.Time          `json:"completedAt,omitempty"`
	Jobs         []JobSummary        `json:"jobs"`
}

// VersionDetailFiles holds the latest uploaded file for each single-file type
// and all uploaded files for jbrowse.track / species.synonym, scoped to a
// single Assembly Version. orthology.tsv is shared across every species in a
// Database Version rather than owned by one assembly, so it is not part of
// this type — it surfaces at the top level of VersionDetail instead.
type VersionDetailFiles struct {
	GenomicFNA     *FileDetail  `json:"genomic.fna"`
	GenomicGFF     *FileDetail  `json:"genomic.gff"`
	RNAFNA         *FileDetail  `json:"rna.fna"`
	CDSFNA         *FileDetail  `json:"cds.fna"`
	ProteinFAA     *FileDetail  `json:"protein.faa"`
	DsRNACSV       *FileDetail  `json:"dsrna.csv"`
	JBrowseTrack   []FileDetail `json:"jbrowse.track"`
	SpeciesSynonym []FileDetail `json:"species.synonym"`
}

// AssemblyVersionDetail is the per-assembly (per-species) representation
// inside VersionDetail, and the response for GET
// /versions/{name}/assemblies/{species}.
type AssemblyVersionDetail struct {
	entity.AssemblyVersion
	Status string             `json:"status"`
	Files  VersionDetailFiles `json:"files"`
}

// VersionDetail is the response for GET /versions/{name}/detail.
type VersionDetail struct {
	entity.Version
	IsDefault     bool                    `json:"isDefault"`
	Status        string                  `json:"status"`
	TotalFileSize int64                   `json:"totalFileSize"`
	OrthologyTSV  []FileDetail            `json:"orthologyTSV"`
	Assemblies    []AssemblyVersionDetail `json:"assemblies"`
}

type UseCase struct {
	versionRepo         IVersionRepository
	appSettingsRepo     IAppSettingsRepository
	jobRepo             IJobRepository
	uploadFileRepo      IUploadFileRepository
	assemblyVersionRepo IAssemblyVersionRepository
	esRepo              IVersionESRepository
	uploadDir           string
}

func New(versionRepo IVersionRepository, appSettingsRepo IAppSettingsRepository, jobRepo IJobRepository, uploadFileRepo IUploadFileRepository, assemblyVersionRepo IAssemblyVersionRepository, esRepo IVersionESRepository, uploadDir string) *UseCase {
	return &UseCase{
		versionRepo:         versionRepo,
		appSettingsRepo:     appSettingsRepo,
		jobRepo:             jobRepo,
		uploadFileRepo:      uploadFileRepo,
		assemblyVersionRepo: assemblyVersionRepo,
		esRepo:              esRepo,
		uploadDir:           uploadDir,
	}
}

func (uc *UseCase) ListVersions(ctx context.Context, page, pageSize int) (*VersionList, error) {
	offset := (page - 1) * pageSize

	versions, err := uc.versionRepo.List(ctx, offset, pageSize)
	if err != nil {
		return nil, err
	}

	total, err := uc.versionRepo.Count(ctx)
	if err != nil {
		return nil, err
	}

	defaultVersionID, err := uc.appSettingsRepo.GetDefaultVersionID(ctx)
	if err != nil {
		return nil, err
	}

	versionIDs := make([]uint64, len(versions))
	for i, v := range versions {
		versionIDs[i] = v.ID
	}

	fileSizes, err := uc.uploadFileRepo.TotalFileSizeByVersionIDs(ctx, versionIDs)
	if err != nil {
		return nil, err
	}

	items := make([]VersionItem, len(versions))
	for i, v := range versions {
		// TODO: N+1 queries for now since batch queries would be more complex to implement.
		// If this becomes a performance bottleneck we can optimize later.
		// Can always cache status for a version if needed to avoid computing on every request.
		status, err := uc.computeVersionRollupStatus(ctx, v.ID)
		if err != nil {
			return nil, err
		}

		items[i] = VersionItem{
			Version:       v,
			IsDefault:     defaultVersionID != nil && *defaultVersionID == v.ID,
			TotalFileSize: fileSizes[v.ID],
			Status:        status,
		}
	}

	return &VersionList{
		Versions: items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

// GetVersionDetail returns full detail for a named Database Version: the
// shared orthology.tsv uploads (not owned by any assembly), one detail entry
// per Assembly Version, and an overall status rolled up across all of them.
func (uc *UseCase) GetVersionDetail(ctx context.Context, name string) (*VersionDetail, error) {
	v, err := uc.versionRepo.FindByName(ctx, name)
	if err != nil {
		return nil, err
	}
	if v == nil {
		return nil, ErrVersionNotFound
	}

	defaultVersionID, err := uc.appSettingsRepo.GetDefaultVersionID(ctx)
	if err != nil {
		return nil, err
	}

	orthologyTSV, err := uc.listOrthologyFileDetails(ctx, v.ID)
	if err != nil {
		return nil, err
	}

	assemblyVersions, err := uc.assemblyVersionRepo.ListByVersionID(ctx, v.ID)
	if err != nil {
		return nil, err
	}

	assemblies := make([]AssemblyVersionDetail, 0, len(assemblyVersions))
	statuses := make([]string, 0, len(assemblyVersions)+1)
	for _, asm := range assemblyVersions {
		ad, err := BuildAssemblyVersionDetail(ctx, uc.jobRepo, uc.uploadFileRepo, asm)
		if err != nil {
			return nil, err
		}
		assemblies = append(assemblies, *ad)
		statuses = append(statuses, ad.Status)
	}

	orthologyStatus, err := uc.computeOrthologyStatus(ctx, v.ID)
	if err != nil {
		return nil, err
	}
	statuses = append(statuses, orthologyStatus)

	status := "MISSING_REQUIRED_FILE"
	if len(assemblyVersions) > 0 {
		status = rollupStatuses(statuses)
	}

	fileSizes, err := uc.uploadFileRepo.TotalFileSizeByVersionIDs(ctx, []uint64{v.ID})
	if err != nil {
		return nil, err
	}

	return &VersionDetail{
		Version:       *v,
		IsDefault:     defaultVersionID != nil && *defaultVersionID == v.ID,
		Status:        status,
		TotalFileSize: fileSizes[v.ID],
		OrthologyTSV:  orthologyTSV,
		Assemblies:    assemblies,
	}, nil
}

// listOrthologyFileDetails returns FileDetail entries for every orthology.tsv
// upload in a Database Version — shared across every species rather than
// owned by one assembly (§2 of the design doc), so this reads all files for
// the whole version (unchanged, existing method) and keeps only orthology.tsv.
func (uc *UseCase) listOrthologyFileDetails(ctx context.Context, versionID uint64) ([]FileDetail, error) {
	files, err := uc.uploadFileRepo.ListByVersionID(ctx, versionID)
	if err != nil {
		return nil, err
	}

	jobs, err := uc.jobRepo.FindByVersionID(ctx, versionID)
	if err != nil {
		return nil, err
	}
	jobsByFileID := make(map[string][]JobSummary)
	for _, j := range jobs {
		if j.FileID == nil {
			continue
		}
		jobsByFileID[*j.FileID] = append(jobsByFileID[*j.FileID], toJobSummary(j))
	}

	orthologyTSV := []FileDetail{}
	for _, f := range files {
		if f.FileType != entity.FileTypeOrthologyTSV {
			continue
		}
		fd := FileDetail{
			ID:           f.ID,
			FilePath:     f.FilePath,
			FileSize:     f.FileSize,
			UploadStatus: f.UploadStatus,
			CreatedAt:    f.CreatedAt,
			CreatedBy:    f.CreatedBy,
			CompletedAt:  f.CompletedAt,
			Jobs:         jobsByFileID[f.ID],
		}
		if fd.Jobs == nil {
			fd.Jobs = []JobSummary{}
		}
		orthologyTSV = append(orthologyTSV, fd)
	}
	return orthologyTSV, nil
}

// BuildAssemblyVersionDetail builds one Assembly Version's detail view
// (status + per-file-type file/job buckets) — shared by GetVersionDetail's
// per-assembly array and usecase/assemblyversion's single-assembly detail
// endpoint, so both stay consistent with each other.
func BuildAssemblyVersionDetail(ctx context.Context, jobRepo IJobRepository, uploadFileRepo IUploadFileRepository, asm entity.AssemblyVersion) (*AssemblyVersionDetail, error) {
	files, err := uploadFileRepo.ListByAssemblyVersionID(ctx, asm.ID)
	if err != nil {
		return nil, err
	}

	jobs, err := jobRepo.FindByAssemblyVersionID(ctx, asm.ID)
	if err != nil {
		return nil, err
	}
	jobsByFileID := make(map[string][]JobSummary)
	for _, j := range jobs {
		if j.FileID == nil {
			continue
		}
		jobsByFileID[*j.FileID] = append(jobsByFileID[*j.FileID], toJobSummary(j))
	}

	// files is ordered by created_at DESC, so the first file of each type is the latest.
	seen := make(map[string]bool)
	detailFiles := VersionDetailFiles{JBrowseTrack: []FileDetail{}, SpeciesSynonym: []FileDetail{}}

	for _, f := range files {
		fd := FileDetail{
			ID:           f.ID,
			FilePath:     f.FilePath,
			FileSize:     f.FileSize,
			UploadStatus: f.UploadStatus,
			CreatedAt:    f.CreatedAt,
			CreatedBy:    f.CreatedBy,
			CompletedAt:  f.CompletedAt,
			Jobs:         jobsByFileID[f.ID],
		}
		if fd.Jobs == nil {
			fd.Jobs = []JobSummary{}
		}

		switch f.FileType {
		case entity.FileTypeJBrowseTrack:
			detailFiles.JBrowseTrack = append(detailFiles.JBrowseTrack, fd)
		case entity.FileTypeSpeciesSynonym:
			detailFiles.SpeciesSynonym = append(detailFiles.SpeciesSynonym, fd)
		default:
			if !seen[f.FileType] {
				seen[f.FileType] = true
				switch f.FileType {
				case entity.FileTypeGenomicFNA:
					detailFiles.GenomicFNA = &fd
				case entity.FileTypeGenomicGFF:
					detailFiles.GenomicGFF = &fd
				case entity.FileTypeRNAFNA:
					detailFiles.RNAFNA = &fd
				case entity.FileTypeCDSFNA:
					detailFiles.CDSFNA = &fd
				case entity.FileTypeProteinFAA:
					detailFiles.ProteinFAA = &fd
				case entity.FileTypeDsRNACSV:
					detailFiles.DsRNACSV = &fd
				}
			}
		}
	}

	counts, err := jobRepo.StatusCountsByAssemblyVersionID(ctx, asm.ID)
	if err != nil {
		return nil, err
	}

	return &AssemblyVersionDetail{
		AssemblyVersion: asm,
		Status:          ComputeVersionStatus(counts, seen[entity.FileTypeGenomicFNA]),
		Files:           detailFiles,
	}, nil
}

func toJobSummary(j entity.Job) JobSummary {
	s := JobSummary{
		ID:          j.ID,
		Type:        j.Type,
		Description: j.Description,
		Status:      j.Status,
		Payload:     j.Payload,
	}
	if j.Status == entity.JobStatusFailed && j.ResultMetadata != nil {
		var meta struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(*j.ResultMetadata, &meta); err == nil && meta.Error != "" {
			s.Error = &meta.Error
		}
	}
	return s
}

func ComputeVersionStatus(c entity.JobStatusCounts, hasRequiredFiles bool) string {
	pendingCount := c.TotalCount - c.RunningCount - c.FailedCount - c.DoneCount
	if c.RunningCount > 0 || pendingCount > 0 {
		return VersionStatusProcessing
	}
	if c.FailedCount > 0 {
		return VersionStatusError
	}
	if !hasRequiredFiles {
		return VersionStatusMissingRequiredFile
	}
	if c.TotalCount > 0 && c.DoneCount == c.TotalCount {
		return VersionStatusReady
	}
	return VersionStatusDraft
}

// computeOrthologyStatus computes the status of a Database Version's shared
// orthology.tsv uploads (never owned by a single Assembly Version, §2 of the
// design doc) — folded into the Database-Version-level status rollup
// alongside each assembly's own status. Unlike an assembly, orthology.tsv is
// never a required file, so an absent upload is READY (nothing to block on),
// not the DRAFT that ComputeVersionStatus's zero-jobs branch would otherwise
// produce.
func (uc *UseCase) computeOrthologyStatus(ctx context.Context, versionID uint64) (string, error) {
	counts, err := uc.jobRepo.StatusCountsForSharedJobs(ctx, versionID)
	if err != nil {
		return "", err
	}
	if counts.TotalCount == 0 {
		return "READY", nil
	}
	return ComputeVersionStatus(counts, true), nil
}

// rollupStatuses combines each Assembly Version's status with the Database
// Version's shared orthology.tsv status into one overall status, per
// docs/design/multi-species-assembly-versions.md §3.
func rollupStatuses(statuses []string) string {
	hasProcessing, hasError, hasMissing, allReady := false, false, false, true
	for _, s := range statuses {
		switch s {
		case "PROCESSING":
			hasProcessing = true
		case "ERROR":
			hasError = true
		case "MISSING_REQUIRED_FILE":
			hasMissing = true
		}
		if s != "READY" {
			allReady = false
		}
	}
	switch {
	case hasProcessing:
		return "PROCESSING"
	case hasError:
		return "ERROR"
	case hasMissing:
		return "MISSING_REQUIRED_FILE"
	case allReady:
		return "READY"
	default:
		return "DRAFT"
	}
}

// computeVersionRollupStatus computes a Database Version's overall status as
// a rollup across each of its Assembly Versions' own status plus the shared
// orthology.tsv status — see docs/design/multi-species-assembly-versions.md
// §3. A Database Version with no Assembly Versions at all cannot be released
// (ReleaseVersion) and reuses MISSING_REQUIRED_FILE to reflect that, the same
// way a single missing genomic.fna does, rather than inventing a 6th status.
func (uc *UseCase) computeVersionRollupStatus(ctx context.Context, versionID uint64) (string, error) {
	assemblyVersions, err := uc.assemblyVersionRepo.ListByVersionID(ctx, versionID)
	if err != nil {
		return "", err
	}
	if len(assemblyVersions) == 0 {
		return "MISSING_REQUIRED_FILE", nil
	}

	orthologyStatus, err := uc.computeOrthologyStatus(ctx, versionID)
	if err != nil {
		return "", err
	}
	statuses := make([]string, 0, len(assemblyVersions)+1)
	statuses = append(statuses, orthologyStatus)

	for _, asm := range assemblyVersions {
		counts, err := uc.jobRepo.StatusCountsByAssemblyVersionID(ctx, asm.ID)
		if err != nil {
			return "", err
		}
		completedFiles, err := uc.uploadFileRepo.FindLatestCompletedPerTypeByAssemblyVersionID(ctx, asm.ID)
		if err != nil {
			return "", err
		}
		hasFNA := false
		for _, f := range completedFiles {
			if f.FileType == entity.FileTypeGenomicFNA {
				hasFNA = true
				break
			}
		}
		statuses = append(statuses, ComputeVersionStatus(counts, hasFNA))
	}

	return rollupStatuses(statuses), nil
}

// ReleaseResult is the response for POST /versions/{name}/release.
type ReleaseResult struct {
	entity.Version
	Jobs []JobSummary `json:"jobs"`
}

// ReleaseVersion enqueues SETUP_BLAST jobs for genomic.fna, protein.faa, and
// rna.fna using the latest completed upload file of each type. Once all three
// blast jobs succeed the worker sets the default version and restarts the blast
// container.
// ReleaseVersion validates every Assembly Version under the Database Version
// has its required files, then enqueues SETUP_BLAST jobs for genomic.fna,
// protein.faa, and rna.fna for each one, using the latest completed upload
// file of each type per assembly. Once all of a version's blast jobs succeed
// the worker sets the default version and restarts the blast container. The
// Database Version's shared orthology.tsv (not owned by any assembly) must
// also have its jobs complete before release.
func (uc *UseCase) ReleaseVersion(ctx context.Context, name string) (*ReleaseResult, error) {
	v, err := uc.versionRepo.FindByName(ctx, name)
	if err != nil {
		return nil, err
	}
	if v == nil {
		return nil, ErrVersionNotFound
	}

	assemblies, err := uc.assemblyVersionRepo.ListByVersionID(ctx, v.ID)
	if err != nil {
		return nil, err
	}
	if len(assemblies) == 0 {
		return nil, fmt.Errorf("%w: no Assembly Versions exist for this Database Version", ErrRequiredFileNotUploaded)
	}

	// The shared orthology.tsv upload (if any) belongs to no single assembly,
	// so its jobs are checked once for the whole Database Version — reusing
	// the existing, unchanged version-scoped query and filtering to orthology.
	sharedFiles, err := uc.uploadFileRepo.FindLatestCompletedPerTypeByVersionID(ctx, v.ID)
	if err != nil {
		return nil, err
	}
	for _, f := range sharedFiles {
		if f.FileType != entity.FileTypeOrthologyTSV {
			continue
		}
		hasNonDone, err := uc.jobRepo.HasNonDoneJobsForFile(ctx, f.ID)
		if err != nil {
			return nil, err
		}
		if hasNonDone {
			return nil, fmt.Errorf("%w: %s", ErrFileJobsNotComplete, f.FileType)
		}
	}

	// removeJobType is empty for file types that are required on every release
	// (currently only genomic.fna) — those can never be absent, so there is no
	// stale database to clean up.
	type blastSpec struct {
		fileType      string
		setupJobType  string
		removeJobType string
	}
	specs := []blastSpec{
		{entity.FileTypeGenomicFNA, entity.JobTypeGenomicFNASetupBlast, ""},
		{entity.FileTypeProteinFAA, entity.JobTypeProteinFAASetupBlast, entity.JobTypeProteinFAARemoveBlast},
		{entity.FileTypeRNAFNA, entity.JobTypeRNAFNASetupBlast, entity.JobTypeRNAFNARemoveBlast},
	}

	var createdJobs []JobSummary
	for _, asm := range assemblies {
		latestFiles, err := uc.uploadFileRepo.FindLatestCompletedPerTypeByAssemblyVersionID(ctx, asm.ID)
		if err != nil {
			return nil, err
		}

		latestByType := make(map[string]*entity.UploadFile, len(latestFiles))
		for i := range latestFiles {
			latestByType[latestFiles[i].FileType] = &latestFiles[i]
		}

		if latestByType[entity.FileTypeGenomicFNA] == nil {
			return nil, fmt.Errorf("%w: %s (species %s)", ErrRequiredFileNotUploaded, entity.FileTypeGenomicFNA, asm.Species)
		}

		// All jobs for the latest completed file of each uploaded type must be DONE.
		for ft, f := range latestByType {
			// TODO: seems like a N+1 query problem. Can we batch this?
			hasNonDone, err := uc.jobRepo.HasNonDoneJobsForFile(ctx, f.ID)
			if err != nil {
				return nil, err
			}
			if hasNonDone {
				return nil, fmt.Errorf("%w: %s (species %s)", ErrFileJobsNotComplete, ft, asm.Species)
			}
		}

		for _, spec := range specs {
			latestFile := latestByType[spec.fileType]

			if latestFile == nil {
				// This assembly has no file of this type. If it was ever built
				// (e.g. by a previous release of this assembly), the on-disk
				// database would otherwise be left stale and keep being served
				// too — enqueue a job to remove it instead.
				if spec.removeJobType == "" {
					continue
				}

				j, err := uc.enqueueRemoveBlastJob(ctx, v.ID, asm.ID, v.Name, spec.removeJobType)
				if err != nil {
					return nil, err
				}
				if j != nil {
					createdJobs = append(createdJobs, toJobSummary(*j))
				}
				continue
			}

			// TODO: seems like a N+1 query problem. Can we batch this?
			// Only skip if one is already in flight (guards against a
			// double-clicked release) — a past DONE job must not block a fresh
			// one, since BLAST DB paths are global and re-releasing this version
			// (e.g. after a different version's release overwrote them) needs to
			// rebuild them from this version's own files again.
			inFlight, err := uc.jobRepo.HasActiveJobOfTypeForAssemblyVersion(ctx, asm.ID, spec.setupJobType)
			if err != nil {
				return nil, err
			}
			if inFlight {
				continue
			}

			rawPayload, err := json.Marshal(jobpayload.SetupBlastPayload{FilePath: latestFile.FilePath, VersionName: v.Name})
			if err != nil {
				return nil, err
			}
			rp := json.RawMessage(rawPayload)
			now := time.Now().UTC()
			assemblyID := asm.ID
			j := &entity.Job{
				VersionID:         v.ID,
				AssemblyVersionID: &assemblyID,
				FileID:            &latestFile.ID,
				Type:              spec.setupJobType,
				Description:       entity.JobDescriptions[spec.setupJobType],
				Payload:           &rp,
				Status:            entity.JobStatusPending,
				CreatedAt:         now,
				UpdatedAt:         now,
			}
			if err := uc.jobRepo.Create(ctx, j); err != nil {
				return nil, err
			}
			createdJobs = append(createdJobs, toJobSummary(*j))
		}
	}

	return &ReleaseResult{Version: *v, Jobs: createdJobs}, nil
}

// enqueueRemoveBlastJob enqueues a job to remove a BLAST database for a file
// type this assembly doesn't have, unless one is already in flight for this
// assembly (mirroring the in-flight-only dedup check for SETUP_BLAST jobs
// above — a past DONE job must not block a fresh one, for the same reason).
func (uc *UseCase) enqueueRemoveBlastJob(ctx context.Context, versionID, assemblyVersionID uint64, versionName string, removeJobType string) (*entity.Job, error) {
	inFlight, err := uc.jobRepo.HasActiveJobOfTypeForAssemblyVersion(ctx, assemblyVersionID, removeJobType)
	if err != nil {
		return nil, err
	}
	if inFlight {
		return nil, nil
	}

	rawPayload, err := json.Marshal(jobpayload.RemoveBlastPayload{VersionName: versionName})
	if err != nil {
		return nil, err
	}
	rp := json.RawMessage(rawPayload)
	now := time.Now().UTC()
	j := &entity.Job{
		VersionID:         versionID,
		AssemblyVersionID: &assemblyVersionID,
		Type:              removeJobType,
		Description:       entity.JobDescriptions[removeJobType],
		Payload:           &rp,
		Status:            entity.JobStatusPending,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := uc.jobRepo.Create(ctx, j); err != nil {
		return nil, err
	}
	return j, nil
}

func (uc *UseCase) CreateVersion(ctx context.Context, name string) (*entity.Version, error) {
	existing, err := uc.versionRepo.FindByName(ctx, name)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, ErrVersionAlreadyExists
	}

	v := &entity.Version{
		Name:      name,
		CreatedBy: auth.UsernameFromContext(ctx),
		UpdatedBy: auth.UsernameFromContext(ctx),
	}

	if err := uc.versionRepo.Create(ctx, v); err != nil {
		return nil, err
	}

	return v, nil
}

func (uc *UseCase) DeleteVersion(ctx context.Context, name string) error {
	v, err := uc.versionRepo.FindByName(ctx, name)
	if err != nil {
		return err
	}
	if v == nil {
		return ErrVersionNotFound
	}

	defaultVersionID, err := uc.appSettingsRepo.GetDefaultVersionID(ctx)
	if err != nil {
		return err
	}
	if defaultVersionID != nil && *defaultVersionID == v.ID {
		return ErrCannotDeleteDefaultVersion
	}

	hasActive, err := uc.jobRepo.HasActiveJobsByVersionID(ctx, v.ID)
	if err != nil {
		return err
	}
	if hasActive {
		return ErrVersionHasActiveJobs
	}

	// Collect file paths before deleting records so we can remove them from disk.
	files, err := uc.uploadFileRepo.ListByVersionID(ctx, v.ID)
	if err != nil {
		return err
	}

	if err := uc.jobRepo.DeleteByVersionID(ctx, v.ID); err != nil {
		return err
	}

	if err := uc.uploadFileRepo.HardDeleteByVersionID(ctx, v.ID); err != nil {
		return err
	}

	// Must run after job/upload_files deletion above — both FK-reference
	// assembly_versions(id).
	if err := uc.assemblyVersionRepo.DeleteByVersionID(ctx, v.ID); err != nil {
		return err
	}

	for _, f := range files {
		if err := os.Remove(f.FilePath); err != nil && !os.IsNotExist(err) {
			log.Ctx(ctx).Warn().Err(err).Str("path", f.FilePath).Msg("failed to remove upload file from disk during version delete")
		}
	}

	versionDir := filepath.Join(uc.uploadDir, v.Name)
	if err := os.RemoveAll(versionDir); err != nil {
		log.Ctx(ctx).Warn().Err(err).Str("path", versionDir).Msg("failed to remove version directory during version delete")
	}

	if err := uc.esRepo.DeleteIndexesByVersion(ctx, v.Name); err != nil {
		log.Ctx(ctx).Warn().Err(err).Str("version", v.Name).Msg("failed to delete ES indexes during version delete")
	}

	if out, err := exec.CommandContext(ctx, deleteJBrowseVersionScript, v.Name).CombinedOutput(); err != nil {
		log.Ctx(ctx).Warn().Err(err).Str("version", v.Name).Str("scriptOutput", string(out)).
			Msg("failed to clean up JBrowse2 data during version delete")
	}

	return uc.versionRepo.Delete(ctx, v.ID)
}
