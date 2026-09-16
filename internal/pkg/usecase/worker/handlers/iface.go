package handlers

import (
	"context"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

type IJobRepository interface {
	Create(ctx context.Context, j *entity.Job) error
	FindDoneByAssemblyVersionAndTypes(ctx context.Context, assemblyVersionID uint64, jobTypes []string) ([]entity.Job, error)
	HasNonFailedJobOfTypeForFile(ctx context.Context, fileID string, jobType string) (bool, error)
	HasDoneJobOfTypeForFile(ctx context.Context, fileID string, jobType string) (bool, error)
	HasNonDoneJobOfTypesForVersion(ctx context.Context, versionID uint64, jobTypes []string) (bool, error)
	FindLatestByFileAndType(ctx context.Context, fileID string, jobType string) (*entity.Job, error)
}

type IUploadFileRepository interface {
	FindLatestCompletedByVersionAndType(ctx context.Context, versionID uint64, fileType string) (*entity.UploadFile, error)
}

type IVersionRepository interface {
	FindByID(ctx context.Context, id uint64) (*entity.Version, error)
}

type IAppSettingsRepository interface {
	SetDefaultVersion(ctx context.Context, versionID uint64) error
	GetDefaultVersionID(ctx context.Context) (*uint64, error)
}

type IAssemblyVersionRepository interface {
	ListByVersionID(ctx context.Context, versionID uint64) ([]entity.AssemblyVersion, error)
}
