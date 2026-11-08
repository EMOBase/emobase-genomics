package version

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"time"

	"github.com/EMOBase/emobase-genomics/internal/pkg/auth"
	"github.com/EMOBase/emobase-genomics/internal/pkg/blastdb"
	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	"github.com/EMOBase/emobase-genomics/internal/pkg/indexname"
	"github.com/EMOBase/emobase-genomics/internal/pkg/jobpayload"
	"github.com/rs/zerolog/log"
)

// deleteJBrowseVersionScript removes a version's assembly, tracks, and
// text-search index entries from the shared JBrowse2 config.json, plus the
// underlying data files (see scripts/delete_jbrowse_version.sh).
const deleteJBrowseVersionScript = "/app/scripts/delete_jbrowse_version.sh"

var (
	ErrVersionAlreadyExists       = errors.New("version already exists")
	ErrInvalidVersionName         = errors.New(`version name must be 1-64 characters of letters, digits, spaces, ".", "_" or "-", starting with a letter or digit and not ending with a space`)
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

// ListVersionsPublic backs GET /public/versions.
//
// Deprecated: only used by the deprecated GET /public/versions; use
// ListVersions instead. Remove together with that endpoint.
func (uc *UseCase) ListVersionsPublic(ctx context.Context, status string) ([]VersionPublicItem, error) {
	versions, err := uc.versionRepo.ListPublic(ctx)
	if err != nil {
		return nil, err
	}
	defaultVersionID, err := uc.appSettingsRepo.GetDefaultVersionID(ctx)
	if err != nil {
		return nil, err
	}
	// TODO: N+1 queries as in ListVersions. Optimize (e.g. cache) if this
	// becomes a bottleneck.
	items := make([]VersionPublicItem, 0, len(versions))
	for _, v := range versions {
		itemStatus, err := uc.computeVersionRollupStatus(ctx, v.ID)
		if err != nil {
			return nil, err
		}
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
// and all uploaded files for jbrowse.track / jbrowse.track.bundle /
// species.synonym, scoped to a single Assembly Version. orthology.tsv is
// shared across every species in a Database Version rather than owned by one
// assembly, so it is not part of this type — it surfaces at the top level of
// VersionDetail instead.
type VersionDetailFiles struct {
	GenomicFNA     *FileDetail  `json:"genomic.fna"`
	GenomicGFF     *FileDetail  `json:"genomic.gff"`
	RNAFNA         *FileDetail  `json:"rna.fna"`
	CDSFNA         *FileDetail  `json:"cds.fna"`
	ProteinFAA     *FileDetail  `json:"protein.faa"`
	DsRNACSV       *FileDetail  `json:"dsrna.csv"`
	JBrowseTrack   []FileDetail `json:"jbrowse.track"`
	SpeciesSynonym []FileDetail `json:"species.synonym"`
	// JBrowseTrackBundle lists the bundle archives, apart from the tracks
	// extracted from them (which are in JBrowseTrack). A bundle that fails
	// validation extracts nothing, so its job is the only place its error shows.
	JBrowseTrackBundle []FileDetail `json:"jbrowse.track.bundle"`
}

// AssemblyVersionDetail is the per-assembly (per-species) representation
// inside VersionDetail, and the response for GET
// /versions/{name}/assemblies/{id}.
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
	// OrthologyBundle lists the orthology bundle archives, apart from the files
	// extracted from them (which are in OrthologyTSV), for the same reason as
	// VersionDetailFiles.JBrowseTrackBundle.
	OrthologyBundle []FileDetail `json:"orthologyBundle"`
}

type UseCase struct {
	versionRepo         IVersionRepository
	appSettingsRepo     IAppSettingsRepository
	jobRepo             IJobRepository
	uploadFileRepo      IUploadFileRepository
	assemblyVersionRepo IAssemblyVersionRepository
	esRepo              IVersionESRepository
	uploadDir           string
	blastDBPath         string
}

func New(versionRepo IVersionRepository, appSettingsRepo IAppSettingsRepository, jobRepo IJobRepository, uploadFileRepo IUploadFileRepository, assemblyVersionRepo IAssemblyVersionRepository, esRepo IVersionESRepository, uploadDir, blastDBPath string) *UseCase {
	return &UseCase{
		versionRepo:         versionRepo,
		appSettingsRepo:     appSettingsRepo,
		jobRepo:             jobRepo,
		uploadFileRepo:      uploadFileRepo,
		assemblyVersionRepo: assemblyVersionRepo,
		esRepo:              esRepo,
		uploadDir:           uploadDir,
		blastDBPath:         blastDBPath,
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

	orthologyTSV, orthologyBundle, err := uc.listOrthologyFileDetails(ctx, v.ID)
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
		Version:         *v,
		IsDefault:       defaultVersionID != nil && *defaultVersionID == v.ID,
		Status:          status,
		TotalFileSize:   fileSizes[v.ID],
		OrthologyTSV:    orthologyTSV,
		OrthologyBundle: orthologyBundle,
		Assemblies:      assemblies,
	}, nil
}

// listOrthologyFileDetails returns FileDetail entries for every orthology.tsv
// and orthology.bundle upload in a Database Version — shared across every
// species rather than owned by one assembly (§2 of the design doc), so this
// reads all files for the whole version and keeps only those two types.
func (uc *UseCase) listOrthologyFileDetails(ctx context.Context, versionID uint64) (tsv, bundles []FileDetail, err error) {
	files, err := uc.uploadFileRepo.ListByVersionID(ctx, versionID)
	if err != nil {
		return nil, nil, err
	}
	// ListByVersionID leaves out bundle rows, so read them on their own.
	// math.MaxInt64 is MySQL's "no limit" for LIMIT.
	bundleFiles, err := uc.uploadFileRepo.ListByVersionIDAndType(ctx, versionID, entity.FileTypeOrthologyBundle, 0, math.MaxInt64)
	if err != nil {
		return nil, nil, err
	}
	files = append(files, bundleFiles...)

	jobs, err := uc.jobRepo.FindByVersionID(ctx, versionID)
	if err != nil {
		return nil, nil, err
	}
	jobsByFileID := make(map[string][]JobSummary)
	for _, j := range jobs {
		if j.FileID == nil {
			continue
		}
		jobsByFileID[*j.FileID] = append(jobsByFileID[*j.FileID], toJobSummary(j))
	}

	tsv, bundles = []FileDetail{}, []FileDetail{}
	for _, f := range files {
		if f.FileType != entity.FileTypeOrthologyTSV && f.FileType != entity.FileTypeOrthologyBundle {
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
		if f.FileType == entity.FileTypeOrthologyBundle {
			bundles = append(bundles, fd)
		} else {
			tsv = append(tsv, fd)
		}
	}
	return tsv, bundles, nil
}

// OrthologyFileList is the response for GET /versions/{name}/orthologies.
type OrthologyFileList struct {
	Files    []FileDetail `json:"files"`
	Total    int          `json:"total"`
	Page     int          `json:"page"`
	PageSize int          `json:"pageSize"`
}

// ListOrthologyFiles returns one page of a Database Version's shared
// orthology.tsv uploads — the same entries as VersionDetail.OrthologyTSV, but
// paged, since an orthology bundle can add many files to one version.
func (uc *UseCase) ListOrthologyFiles(ctx context.Context, name string, page, pageSize int) (*OrthologyFileList, error) {
	v, err := uc.versionRepo.FindByName(ctx, name)
	if err != nil {
		return nil, err
	}
	if v == nil {
		return nil, ErrVersionNotFound
	}

	files, err := uc.uploadFileRepo.ListByVersionIDAndType(ctx, v.ID, entity.FileTypeOrthologyTSV, (page-1)*pageSize, pageSize)
	if err != nil {
		return nil, err
	}

	total, err := uc.uploadFileRepo.CountByVersionIDAndType(ctx, v.ID, entity.FileTypeOrthologyTSV)
	if err != nil {
		return nil, err
	}

	fileIDs := make([]string, len(files))
	for i, f := range files {
		fileIDs[i] = f.ID
	}
	jobs, err := uc.jobRepo.FindByFileIDs(ctx, fileIDs)
	if err != nil {
		return nil, err
	}
	jobsByFileID := make(map[string][]JobSummary)
	for _, j := range jobs {
		jobsByFileID[*j.FileID] = append(jobsByFileID[*j.FileID], toJobSummary(j))
	}

	details := make([]FileDetail, len(files))
	for i, f := range files {
		details[i] = FileDetail{
			ID:           f.ID,
			FilePath:     f.FilePath,
			FileSize:     f.FileSize,
			UploadStatus: f.UploadStatus,
			CreatedAt:    f.CreatedAt,
			CreatedBy:    f.CreatedBy,
			CompletedAt:  f.CompletedAt,
			Jobs:         jobsByFileID[f.ID],
		}
		if details[i].Jobs == nil {
			details[i].Jobs = []JobSummary{}
		}
	}

	return &OrthologyFileList{
		Files:    details,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
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
	detailFiles := VersionDetailFiles{JBrowseTrack: []FileDetail{}, JBrowseTrackBundle: []FileDetail{}, SpeciesSynonym: []FileDetail{}}

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
		case entity.FileTypeJBrowseTrackBundle:
			detailFiles.JBrowseTrackBundle = append(detailFiles.JBrowseTrackBundle, fd)
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
		Status:          ComputeVersionStatus(counts),
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

// ComputeVersionStatus derives a status from job counts alone: no file type is
// required, so it is never MISSING_REQUIRED_FILE (that is only reported for a
// Database Version with zero Assembly Versions, see computeVersionRollupStatus).
func ComputeVersionStatus(c entity.JobStatusCounts) string {
	pendingCount := c.TotalCount - c.RunningCount - c.FailedCount - c.DoneCount
	if c.RunningCount > 0 || pendingCount > 0 {
		return VersionStatusProcessing
	}
	if c.FailedCount > 0 {
		return VersionStatusError
	}
	if c.TotalCount > 0 && c.DoneCount == c.TotalCount {
		return VersionStatusReady
	}
	return VersionStatusDraft
}

// computeOrthologyStatus computes the status of a Database Version's shared
// orthology.tsv uploads (never owned by a single Assembly Version, §2 of the
// design doc) — folded into the Database-Version-level status rollup
// alongside each assembly's own status. An absent orthology.tsv upload is
// READY (nothing to block on), not the DRAFT that ComputeVersionStatus's
// zero-jobs branch would otherwise produce.
func (uc *UseCase) computeOrthologyStatus(ctx context.Context, versionID uint64) (string, error) {
	counts, err := uc.jobRepo.StatusCountsForSharedJobs(ctx, versionID)
	if err != nil {
		return "", err
	}
	if counts.TotalCount == 0 {
		return "READY", nil
	}
	return ComputeVersionStatus(counts), nil
}

// rollupStatuses combines each Assembly Version's status with the Database
// Version's shared orthology.tsv status into one overall status, per
// docs/design/multi-species-assembly-versions.md §3.
func rollupStatuses(statuses []string) string {
	hasProcessing, hasError, allReady := false, false, true
	for _, s := range statuses {
		switch s {
		case "PROCESSING":
			hasProcessing = true
		case "ERROR":
			hasError = true
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
// (ReleaseVersion) and reports MISSING_REQUIRED_FILE to reflect that, rather
// than inventing a 6th status.
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
		statuses = append(statuses, ComputeVersionStatus(counts))
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
// ReleaseVersion enqueues, for every Assembly Version under the Database
// Version, SETUP_BLAST jobs for genomic.fna, protein.faa, and rna.fna using
// the latest completed upload file of each type per assembly — or a
// REMOVE_BLAST job for a type the assembly has no file of, since no file type
// is required. Once all of a version's blast jobs succeed
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

	// Orthology belongs to no single assembly, so its files are checked once
	// for the whole Database Version: every orthology.tsv, since one failing
	// must not hide behind a newer one, and the latest orthology.bundle.
	sharedFiles, err := uc.uploadFileRepo.ListCurrentCompletedSharedByVersionID(ctx, v.ID)
	if err != nil {
		return nil, err
	}
	for _, f := range sharedFiles {
		hasNonDone, err := uc.jobRepo.HasNonDoneJobsForFile(ctx, f.ID)
		if err != nil {
			return nil, err
		}
		if hasNonDone {
			return nil, fmt.Errorf("%w: %s", ErrFileJobsNotComplete, f.FileType)
		}
	}

	type blastSpec struct {
		fileType      string
		setupJobType  string
		removeJobType string
	}
	specs := []blastSpec{
		{entity.FileTypeGenomicFNA, entity.JobTypeGenomicFNASetupBlast, entity.JobTypeGenomicFNARemoveBlast},
		{entity.FileTypeProteinFAA, entity.JobTypeProteinFAASetupBlast, entity.JobTypeProteinFAARemoveBlast},
		{entity.FileTypeRNAFNA, entity.JobTypeRNAFNASetupBlast, entity.JobTypeRNAFNARemoveBlast},
	}

	var createdJobs []JobSummary
	for _, asm := range assemblies {
		files, err := uc.uploadFileRepo.ListCurrentCompletedByAssemblyVersionID(ctx, asm.ID)
		if err != nil {
			return nil, err
		}

		// Only read for the BLAST types below, which keep one file per assembly.
		latestByType := make(map[string]*entity.UploadFile, len(files))
		for i := range files {
			latestByType[files[i].FileType] = &files[i]
		}

		// All jobs of every current file must be DONE, including each of an
		// assembly's many jbrowse.track and species.synonym files.
		for _, f := range files {
			// TODO: seems like a N+1 query problem. Can we batch this?
			hasNonDone, err := uc.jobRepo.HasNonDoneJobsForFile(ctx, f.ID)
			if err != nil {
				return nil, err
			}
			if hasNonDone {
				return nil, fmt.Errorf("%w: %s (species %s)", ErrFileJobsNotComplete, f.FileType, asm.SpeciesCode)
			}
		}

		for _, spec := range specs {
			latestFile := latestByType[spec.fileType]

			if latestFile == nil {
				// This assembly has no file of this type. If it was ever built
				// (e.g. by a previous release of this assembly), the on-disk
				// database would otherwise be left stale and keep being served
				// too — enqueue a job to remove it instead.
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

			rawPayload, err := json.Marshal(jobpayload.SetupBlastPayload{
				FilePath:          latestFile.FilePath,
				VersionID:         v.ID,
				AssemblyVersionID: asm.ID,
				AssemblyName:      asm.Name,
				VersionName:       v.Name,
			})
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

	rawPayload, err := json.Marshal(jobpayload.RemoveBlastPayload{
		VersionID:         versionID,
		AssemblyVersionID: assemblyVersionID,
		VersionName:       versionName,
	})
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

// versionNamePattern keeps a version name one path segment, never "." or
// "..", so the version's upload folder (removed recursively on delete) stays
// inside the upload directory. For such a name, indexname.FromVersionName
// only lowercases it and turns each run of spaces into "_".
var versionNamePattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9 ._-]{0,62}[A-Za-z0-9._-])?$`)

func (uc *UseCase) CreateVersion(ctx context.Context, name string) (*entity.Version, error) {
	if !versionNamePattern.MatchString(name) {
		return nil, ErrInvalidVersionName
	}

	// Two versions with the same index slug would share every Elasticsearch
	// index and alias, and deleting one would delete both versions' indexes.
	// So compare slugs, not names: that ignores letter case and counts a run of
	// spaces as "_", exactly as the index names do.
	names, err := uc.versionRepo.ListNames(ctx)
	if err != nil {
		return nil, err
	}
	slug := indexname.FromVersionName(name)
	for _, n := range names {
		if indexname.FromVersionName(n) != slug {
			continue
		}
		if n == name {
			return nil, ErrVersionAlreadyExists
		}
		return nil, fmt.Errorf(`%w: %q is the same as version %q when letter case is ignored and spaces count as "_"`,
			ErrVersionAlreadyExists, name, n)
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

	// Collect file paths and assemblies before deleting records: files so we
	// can remove them from disk, assemblies so we can clean up each one's
	// JBrowse2 data (below) after their MySQL rows are gone.
	files, err := uc.uploadFileRepo.ListByVersionID(ctx, v.ID)
	if err != nil {
		return err
	}
	assemblies, err := uc.assemblyVersionRepo.ListByVersionID(ctx, v.ID)
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

	// Not the default version (checked above), so its BLAST databases are not
	// served; a release that failed part-way can still have left some.
	if _, err := blastdb.RemoveVersion(uc.blastDBPath, v.ID); err != nil {
		log.Ctx(ctx).Warn().Err(err).Str("version", v.Name).Msg("failed to remove BLAST databases during version delete")
	}

	if err := uc.esRepo.DeleteIndexesByVersion(ctx, v.Name, v.ID); err != nil {
		log.Ctx(ctx).Warn().Err(err).Str("version", v.Name).Msg("failed to delete ES indexes during version delete")
	}

	// Every assembly gets its own JBrowse2 assembly/tracks (§5 of the design
	// doc); deleting a whole Database Version means running the same
	// single-assembly delete script once per assembly it had.
	for _, asm := range assemblies {
		if out, err := exec.CommandContext(ctx, deleteJBrowseVersionScript, asm.AssemblyID()).CombinedOutput(); err != nil {
			log.Ctx(ctx).Warn().Err(err).Str("version", v.Name).Str("assemblyID", asm.AssemblyID()).Str("scriptOutput", string(out)).
				Msg("failed to clean up JBrowse2 data during version delete")
		}
	}

	return uc.versionRepo.Delete(ctx, v.ID)
}
