package sequence

import (
	"context"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

type ISequenceRepository interface {
	SaveMany(ctx context.Context, indexName string, seqs []entity.Sequence) error
}
