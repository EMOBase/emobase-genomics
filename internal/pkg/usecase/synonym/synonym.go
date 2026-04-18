package synonym

import (
	"context"
	"io"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	synonymparser "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/synonym/parser"
	"github.com/rs/zerolog/log"
)

type SynonymUseCase struct {
	config Config
	repo   ISynonymRepository
}

func New(repo ISynonymRepository) *SynonymUseCase {
	return &SynonymUseCase{
		config: Config{BatchSize: 1000},
		repo:   repo,
	}
}

func (uc *SynonymUseCase) Load(ctx context.Context, f io.Reader, indexName string, p synonymparser.ISynonymParser) error {
	ctx, ctxCancel := context.WithCancel(ctx)
	defer ctxCancel()

	synonymCh, errCh := p.Parse(ctx, f)

	count := 0
	batch := make([]entity.Synonym, 0, uc.config.BatchSize)

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := uc.repo.SaveMany(ctx, indexName, batch); err != nil {
			return err
		}
		count += len(batch)
		batch = batch[:0]
		return nil
	}

	for s := range synonymCh {
		batch = append(batch, s)
		if len(batch) >= uc.config.BatchSize {
			if err := flush(); err != nil {
				return err
			}
		}
	}

	if err := flush(); err != nil {
		return err
	}

	if err := <-errCh; err != nil {
		return err
	}

	log.Ctx(ctx).Info().Int("total", count).Msg("synonyms loaded")

	return nil
}
