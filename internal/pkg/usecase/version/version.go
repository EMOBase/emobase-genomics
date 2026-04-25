package version

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/EMOBase/emobase-genomics/internal/pkg/auth"
	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	"github.com/EMOBase/emobase-genomics/internal/pkg/jobpayload"
)

var (
	ErrVersionAlreadyExists = errors.New("version already exists")
	ErrVersionNotFound      = errors.New("version not found")
)

type VersionItem struct {
	entity.Version
	IsDefault     bool   `json:"isDefault"`
	Status        string `json:"status"`
	TotalFileSize int64  `json:"totalFileSize"`
}

type VersionList struct {
	Versions []VersionItem `json:"versions"`
	Total    int           `json:"total"`
	Page     int           `json:"page"`
	PageSize int           `json:"pageSize"`
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
// and all uploaded files for orthology.
type VersionDetailFiles struct {
	GenomicFNA   *FileDetail  `json:"genomic.fna"`
	GenomicGFF   *FileDetail  `json:"genomic.gff"`
	RNAFNA       *FileDetail  `json:"rna.fna"`
	CDSFNA       *FileDetail  `json:"cds.fna"`
	ProteinFAA   *FileDetail  `json:"protein.faa"`
	OrthologyTSV []FileDetail `json:"orthology.tsv"`
}

// VersionDetail is the response for GET /versions/{name}/detail.
type VersionDetail struct {
	entity.Version
	IsDefault bool               `json:"isDefault"`
	Status    string             `json:"status"`
	Files     VersionDetailFiles `json:"files"`
}

type UseCase struct {
	versionRepo     IVersionRepository
	appSettingsRepo IAppSettingsRepository
	jobRepo         IJobRepository
	uploadFileRepo  IUploadFileRepository
}

func New(versionRepo IVersionRepository, appSettingsRepo IAppSettingsRepository, jobRepo IJobRepository, uploadFileRepo IUploadFileRepository) *UseCase {
	return &UseCase{versionRepo: versionRepo, appSettingsRepo: appSettingsRepo, jobRepo: jobRepo, uploadFileRepo: uploadFileRepo}
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

	statusCounts, err := uc.jobRepo.StatusCountsByVersionIDs(ctx, versionIDs)
	if err != nil {
		return nil, err
	}

	fileSizes, err := uc.uploadFileRepo.TotalFileSizeByVersionIDs(ctx, versionIDs)
	if err != nil {
		return nil, err
	}

	items := make([]VersionItem, len(versions))
	for i, v := range versions {
		items[i] = VersionItem{
			Version:       v,
			IsDefault:     defaultVersionID != nil && *defaultVersionID == v.ID,
			Status:        computeVersionStatus(statusCounts[v.ID]),
			TotalFileSize: fileSizes[v.ID],
		}
	}

	return &VersionList{
		Versions: items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

// GetVersionDetail returns full detail for a named version: version info, the
// latest uploaded file per single-file type, all orthology files, and each
// file's associated jobs.
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

	files, err := uc.uploadFileRepo.ListByVersionID(ctx, v.ID)
	if err != nil {
		return nil, err
	}

	jobs, err := uc.jobRepo.FindByVersionID(ctx, v.ID)
	if err != nil {
		return nil, err
	}

	// Index jobs by file_id for O(1) lookup.
	jobsByFileID := make(map[string][]JobSummary)
	for _, j := range jobs {
		if j.FileID == nil {
			continue
		}
		jobsByFileID[*j.FileID] = append(jobsByFileID[*j.FileID], toJobSummary(j))
	}

	// files is ordered by created_at DESC, so the first file of each type is the latest.
	// For orthology, collect all; for others, keep only the first seen.
	seen := make(map[string]bool)
	detail := VersionDetail{
		Version:   *v,
		IsDefault: defaultVersionID != nil && *defaultVersionID == v.ID,
		Files:     VersionDetailFiles{OrthologyTSV: []FileDetail{}},
	}

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
		case entity.FileTypeOrthologyTSV:
			detail.Files.OrthologyTSV = append(detail.Files.OrthologyTSV, fd)
		default:
			if !seen[f.FileType] {
				seen[f.FileType] = true
				switch f.FileType {
				case entity.FileTypeGenomicFNA:
					detail.Files.GenomicFNA = &fd
				case entity.FileTypeGenomicGFF:
					detail.Files.GenomicGFF = &fd
				case entity.FileTypeRNAFNA:
					detail.Files.RNAFNA = &fd
				case entity.FileTypeCDSFNA:
					detail.Files.CDSFNA = &fd
				case entity.FileTypeProteinFAA:
					detail.Files.ProteinFAA = &fd
				}
			}
		}
	}

	statusCounts, err := uc.jobRepo.StatusCountsByVersionIDs(ctx, []uint64{v.ID})
	if err != nil {
		return nil, err
	}
	detail.Status = computeVersionStatus(statusCounts[v.ID])

	return &detail, nil
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

func computeVersionStatus(c entity.JobStatusCounts) string {
	if c.FailedCount > 0 {
		return "ERROR"
	}
	if c.RunningCount > 0 {
		return "PROCESSING"
	}
	if c.TotalCount > 0 && c.DoneCount == c.TotalCount {
		return "READY"
	}
	return "DRAFT"
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
func (uc *UseCase) ReleaseVersion(ctx context.Context, name string) (*ReleaseResult, error) {
	v, err := uc.versionRepo.FindByName(ctx, name)
	if err != nil {
		return nil, err
	}
	if v == nil {
		return nil, ErrVersionNotFound
	}

	files, err := uc.uploadFileRepo.ListByVersionID(ctx, v.ID)
	if err != nil {
		return nil, err
	}

	// files is ordered by created_at DESC — first COMPLETED match per type is the latest.
	type blastSpec struct {
		fileType string
		jobType  string
	}
	specs := []blastSpec{
		{entity.FileTypeGenomicFNA, entity.JobTypeGenomicFNASetupBlast},
		{entity.FileTypeProteinFAA, entity.JobTypeProteinFAASetupBlast},
		{entity.FileTypeRNAFNA, entity.JobTypeRNAFNASetupBlast},
	}

	var createdJobs []JobSummary
	for _, spec := range specs {
		var latestFile *entity.UploadFile
		for i := range files {
			if files[i].FileType == spec.fileType && files[i].UploadStatus == entity.UploadStatusCompleted {
				latestFile = &files[i]
				break
			}
		}
		if latestFile == nil {
			continue
		}

		exists, err := uc.jobRepo.HasNonFailedJobOfType(ctx, v.ID, spec.jobType)
		if err != nil {
			return nil, err
		}
		if exists {
			continue
		}

		rawPayload, err := json.Marshal(jobpayload.SetupBlastPayload{FilePath: latestFile.FilePath})
		if err != nil {
			return nil, err
		}
		rp := json.RawMessage(rawPayload)
		now := time.Now().UTC()
		j := &entity.Job{
			VersionID:   v.ID,
			FileID:      &latestFile.ID,
			Type:        spec.jobType,
			Description: entity.JobDescriptions[spec.jobType],
			Payload:     &rp,
			Status:      entity.JobStatusPending,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if err := uc.jobRepo.Create(ctx, j); err != nil {
			return nil, err
		}
		createdJobs = append(createdJobs, toJobSummary(*j))
	}

	return &ReleaseResult{Version: *v, Jobs: createdJobs}, nil
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
