package job

import (
	"context"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

type IJobRepository interface {
	FindByVersionID(ctx context.Context, versionID uint64) ([]entity.Job, error)
}

type IVersionRepository interface {
	FindByName(ctx context.Context, name string) (*entity.Version, error)
}
