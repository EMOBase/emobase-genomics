package orthology

import (
	"context"
	"io"
	"strings"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	"github.com/EMOBase/emobase-genomics/internal/pkg/parser/text"
	"github.com/rs/zerolog/log"
)

type OrthologyUseCase struct {
	config Config
	repo   IOrthologyRepository
}

func New(repo IOrthologyRepository) *OrthologyUseCase {
	return &OrthologyUseCase{
		config: Config{BatchSize: 1000},
		repo:   repo,
	}
}

func (uc *OrthologyUseCase) Load(ctx context.Context, f io.Reader, indexName, order, algorithm string) error {
	ctx, ctxCancel := context.WithCancel(ctx)
	defer ctxCancel()

	orthoSource := order + "." + algorithm

	lineCh, errCh := text.ReadLines(ctx, f)

	count := 0
	batch := make([]entity.Orthology, 0, uc.config.BatchSize)
	isFirstLine := true
	var species []string

	for {
		line, ok := <-lineCh

		if !ok && len(batch) > 0 {
			if err := uc.repo.SaveMany(ctx, indexName, batch); err != nil {
				return err
			}
			log.Info().Int("batch", len(batch)).Int("total", count).Msg("inserted last batch of orthologies")
			break
		}

		if !ok {
			break
		}

		if line == "" {
			continue
		}

		if isFirstLine {
			species = strings.Split(line, delimiter)[1:]
			isFirstLine = false
			continue
		}

		count++

		cols := strings.Split(line, delimiter)
		if len(cols) != 3 {
			return ErrInvalidOrthologyFormat
		}

		orthology := entity.Orthology{
			Group: orthoSource + ":" + cols[0],
		}

		for i := 1; i < len(cols); i++ {
			sp := species[i-1]
			for gene := range strings.SplitSeq(strings.TrimSpace(cols[i]), ",") {
				gene = strings.TrimSpace(gene)
				if gene == "" {
					continue
				}
				orthology.Orthologs = append(orthology.Orthologs, sp+":"+gene)
			}
		}

		batch = append(batch, orthology)

		if len(batch) < uc.config.BatchSize {
			continue
		}

		if err := uc.repo.SaveMany(ctx, indexName, batch); err != nil {
			return err
		}
		log.Info().Int("batch", len(batch)).Int("total", count).Msg("inserted orthologies batch")
		batch = batch[:0]
	}

	ctxCancel()

	if err := <-errCh; err != nil {
		return err
	}

	return nil
}
