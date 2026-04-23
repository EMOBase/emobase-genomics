package upload

import (
	"context"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

type IVersionRepository interface {
	FindByName(ctx context.Context, name string) (*entity.Version, error)
}

type IJobRepository interface {
	Create(ctx context.Context, j *entity.Job) error
	HasActiveJobOfType(ctx context.Context, versionID uint64, jobType string) (bool, error)
	HasActiveJobOfTypeForFile(ctx context.Context, fileID string, jobType string) (bool, error)
	HasDoneJobOfType(ctx context.Context, versionID uint64, jobType string) (bool, error)
	HasNonFailedJobOfTypeForFile(ctx context.Context, fileID string, jobType string) (bool, error)
}

type IUploadFileRepository interface {
	Create(ctx context.Context, f *entity.UploadFile) error
	FindByID(ctx context.Context, id string) (*entity.UploadFile, error)
	UpdateStatus(ctx context.Context, id string, status entity.UploadStatus) error
	SoftDelete(ctx context.Context, id string, deletedBy string) error
	TotalFileSizeByVersionIDs(ctx context.Context, versionIDs []uint64) (map[uint64]int64, error)
}
