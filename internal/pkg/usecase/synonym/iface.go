package synonym

import (
	"context"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

type ISynonymRepository interface {
	SaveMany(ctx context.Context, synonyms []entity.Synonym) error
}
