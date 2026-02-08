package genomic

import (
	"context"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

type IGenomicLocationRepository interface {
	SaveMany(ctx context.Context, locs []entity.GenomicLocation) error
}
