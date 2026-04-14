package handlers

import (
	"context"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

type IJobRepository interface {
	Create(ctx context.Context, j *entity.Job) error
}
