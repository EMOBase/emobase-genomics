package sequence

import (
	"context"
	"io"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	"github.com/EMOBase/emobase-genomics/internal/pkg/parser/fasta"
	"github.com/rs/zerolog/log"
)

type SequenceUseCase struct {
	config Config
	repo   ISequenceRepository
}

func New(repo ISequenceRepository, mainSpecies string) *SequenceUseCase {
	return &SequenceUseCase{
		config: Config{MainSpecies: mainSpecies, BatchSize: 5000},
		repo:   repo,
	}
}

func (uc *SequenceUseCase) Load(ctx context.Context, f io.Reader, indexName, sequenceType string) error {
	ctx, ctxCancel := context.WithCancel(ctx)
	defer ctxCancel()

	recordCh, errCh := fasta.ReadFastaRecords(ctx, f)
	count := 0
	batch := make([]entity.Sequence, 0, uc.config.BatchSize)

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

	for record := range recordCh {
		batch = append(batch, entity.Sequence{
			Name:     record.Header,
			Sequence: record.Sequence,
			Type:     sequenceType,
			Species:  uc.config.MainSpecies,
		})
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

	log.Ctx(ctx).Info().Int("total", count).Msg("sequences loaded")

	return nil
}
