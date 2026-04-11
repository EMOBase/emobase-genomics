package parser

import (
	"context"
	"io"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

// ISynonymParser is implemented by each file-format-specific synonym parser.
// Parse streams entity.Synonym records from the given reader until EOF or ctx cancellation.
type ISynonymParser interface {
	Parse(ctx context.Context, r io.Reader) (<-chan entity.Synonym, <-chan error)
}
