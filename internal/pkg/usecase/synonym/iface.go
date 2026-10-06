package synonym

import (
	"context"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

type ISynonymRepository interface {
	SaveMany(ctx context.Context, indexName string, synonyms []entity.Synonym) error
	DeleteByFileID(ctx context.Context, indexName, fileID string) error
}
