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
}

type IUploadFileRepository interface {
	Create(ctx context.Context, f *entity.UploadFile) error
	UpdateStatus(ctx context.Context, id string, status entity.UploadStatus) error
}
