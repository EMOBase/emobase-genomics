package job

import (
	"context"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

type IJobRepository interface {
	FindByID(ctx context.Context, id uint64) (*entity.Job, error)
}
