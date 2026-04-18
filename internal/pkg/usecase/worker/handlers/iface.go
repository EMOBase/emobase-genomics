package handlers

import (
	"context"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

type IJobRepository interface {
	Create(ctx context.Context, j *entity.Job) error
	FindDoneByVersionAndTypes(ctx context.Context, versionID uint64, jobTypes []string) ([]entity.Job, error)
	HasNonFailedJobOfType(ctx context.Context, versionID uint64, jobType string) (bool, error)
}

type IVersionRepository interface {
	FindByID(ctx context.Context, id uint64) (*entity.Version, error)
}
