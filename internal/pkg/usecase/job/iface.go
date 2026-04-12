package job

import (
	"context"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

type IJobRepository interface {
	FindByVersionName(ctx context.Context, versionName string) ([]entity.Job, error)
}
