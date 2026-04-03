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
		config: Config{MainSpecies: mainSpecies, BatchSize: 1000},
		repo:   repo,
	}
}

func (uc *SequenceUseCase) Load(ctx context.Context, f io.Reader, indexName, sequenceType string) error {
	ctx, ctxCancel := context.WithCancel(ctx)
	defer ctxCancel()

	recordCh, errCh := fasta.ReadFastaRecords(ctx, f)
	count := 0
	batch := make([]entity.Sequence, 0, uc.config.BatchSize)

	for {
		fastaRecord, ok := <-recordCh

		if !ok && len(batch) > 0 {
			if err := uc.repo.SaveMany(ctx, indexName, batch); err != nil {
				return err
			}
			log.Info().Int("batch", len(batch)).Int("total", count).Msg("inserted last batch of sequences")
			break
		}

		if !ok {
			break
		}

		count++
		batch = append(batch, entity.Sequence{
			Name:     fastaRecord.Header,
			Sequence: fastaRecord.Sequence,
			Type:     sequenceType,
			Species:  uc.config.MainSpecies,
		})

		if len(batch) < uc.config.BatchSize {
			continue
		}

		if err := uc.repo.SaveMany(ctx, indexName, batch); err != nil {
			return err
		}
		log.Info().Int("batch", len(batch)).Int("total", count).Msg("inserted sequences batch")
		batch = batch[:0]
	}

	ctxCancel()

	if err := <-errCh; err != nil {
		return err
	}

	return nil
}
