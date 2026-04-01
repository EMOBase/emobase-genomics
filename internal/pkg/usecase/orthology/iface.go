package orthology

import (
	"context"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

type IOrthologyRepository interface {
	SaveMany(ctx context.Context, locs []entity.Orthology) error
}
