package sequence

import (
	"context"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

type ISequenceRepository interface {
	SaveMany(ctx context.Context, seqs []entity.Sequence) error
}
