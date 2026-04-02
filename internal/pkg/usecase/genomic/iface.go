package genomic

import (
	"context"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

type IGenomicLocationRepository interface {
	SaveMany(ctx context.Context, indexName string, locs []entity.GenomicLocation) error
}
