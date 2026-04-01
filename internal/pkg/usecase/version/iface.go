package version

import (
	"context"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

type IVersionRepository interface {
	Create(ctx context.Context, v *entity.Version) error
	FindByName(ctx context.Context, name string) (*entity.Version, error)
}
