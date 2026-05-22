package search

import (
	"context"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

type ISynonymRepository interface {
	FindBySynonymRelaxed(ctx context.Context, indexName, query string) ([]entity.Synonym, error)
	FindByGenes(ctx context.Context, indexName string, genes []string) ([]entity.Synonym, error)
}

type IOrthologyRepository interface {
	ListByOrthologs(ctx context.Context, indexName string, genes []string) ([]entity.Orthology, error)
}
