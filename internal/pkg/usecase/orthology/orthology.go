package orthology

import (
	"context"
	"fmt"
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
		config: Config{BatchSize: 5000},
		repo:   repo,
	}
}

func (uc *OrthologyUseCase) Load(ctx context.Context, f io.Reader, indexName, order, algorithm string) error {
	ctx, ctxCancel := context.WithCancel(ctx)
	defer ctxCancel()

	orthoSource := order + "." + algorithm

	lineCh, errCh := text.ReadLines(ctx, f)

	count := 0
	lineNum := 0
	batch := make([]entity.Orthology, 0, uc.config.BatchSize)
	isFirstLine := true
	var species []string

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

	for line := range lineCh {
		lineNum++

		if line == "" {
			continue
		}

		if isFirstLine {
			species = strings.Split(line, delimiter)[1:]
			isFirstLine = false
			continue
		}

		cols := strings.Split(line, delimiter)
		if len(cols) != 3 {
			return fmt.Errorf("line %d: %w", lineNum, ErrInvalidOrthologyFormat)
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

	log.Ctx(ctx).Info().Int("total", count).Msg("orthologies loaded")

	return nil
}
