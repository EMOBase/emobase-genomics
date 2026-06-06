package dsrna

import (
	"context"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

type IDsRNARepository interface {
	SaveMany(ctx context.Context, indexName string, docs []entity.DsRNA) error
}
