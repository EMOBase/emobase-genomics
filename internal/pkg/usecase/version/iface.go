package version

import (
	"context"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

type IVersionRepository interface {
	Create(ctx context.Context, v *entity.Version) error
	FindByName(ctx context.Context, name string) (*entity.Version, error)
	List(ctx context.Context, offset, limit int) ([]entity.Version, error)
	Count(ctx context.Context) (int, error)
}

type IAppSettingsRepository interface {
	SetDefaultVersion(ctx context.Context, versionID uint64) error
	GetDefaultVersionID(ctx context.Context) (*uint64, error)
}

type IJobRepository interface {
	StatusCountsByVersionIDs(ctx context.Context, versionIDs []uint64) (map[uint64]entity.JobStatusCounts, error)
}
