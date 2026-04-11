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

	for {
		s, ok := <-synonymCh

		if !ok && len(batch) > 0 {
			if err := uc.repo.SaveMany(ctx, indexName, batch); err != nil {
				return err
			}
			log.Info().Int("batch", len(batch)).Int("total", count).Msg("inserted last batch of synonyms")
			break
		}

		if !ok {
			break
		}

		count++
		batch = append(batch, s)

		if len(batch) < uc.config.BatchSize {
			continue
		}

		if err := uc.repo.SaveMany(ctx, indexName, batch); err != nil {
			return err
		}
		log.Info().Int("batch", len(batch)).Int("total", count).Msg("inserted synonyms batch")
		batch = batch[:0]
	}

	ctxCancel()

	if err := <-errCh; err != nil {
		return err
	}

	return nil
}
