package orthology

import (
	"context"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

type IOrthologyRepository interface {
	SaveMany(ctx context.Context, indexName string, orthologies []entity.Orthology) error
	SetAlias(ctx context.Context, indexName, aliasName string) error
}
