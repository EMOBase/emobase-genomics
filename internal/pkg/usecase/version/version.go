package version

import (
	"context"
	"errors"

	"github.com/EMOBase/emobase-genomics/internal/pkg/auth"
	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
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

type UseCase struct {
	versionRepo    IVersionRepository
	appSettingsRepo IAppSettingsRepository
	jobRepo        IJobRepository
	uploadFileRepo IUploadFileRepository
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

func computeVersionStatus(c entity.JobStatusCounts) string {
	if c.FailedCount > 0 {
		return "error"
	}
	if c.RunningCount > 0 {
		return "processing"
	}
	if c.TotalCount > 0 && c.DoneCount == c.TotalCount {
		return "ready"
	}
	return "draft"
}

func (uc *UseCase) SetDefaultVersion(ctx context.Context, name string) (*entity.Version, error) {
	v, err := uc.versionRepo.FindByName(ctx, name)
	if err != nil {
		return nil, err
	}
	if v == nil {
		return nil, ErrVersionNotFound
	}

	if err := uc.appSettingsRepo.SetDefaultVersion(ctx, v.ID); err != nil {
		return nil, err
	}

	return v, nil
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
