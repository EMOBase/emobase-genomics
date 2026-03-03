package orthology

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	"github.com/EMOBase/emobase-genomics/internal/pkg/parser/text"
)

type OrthologyUseCase struct {
	config              Config
	orthologyRepository IOrthologyRepository
}

func NewOrthologyUseCase(
	orthologyRepository IOrthologyRepository,
) *OrthologyUseCase {
	return &OrthologyUseCase{
		config:              Config{BatchSize: 1000},
		orthologyRepository: orthologyRepository,
	}
}

func (uc *OrthologyUseCase) Load(ctx context.Context, f *os.File) error {
	ctx, ctxCancel := context.WithCancel(ctx)
	defer ctxCancel()

	orthoPrefix := filepath.Base(f.Name())
	for _, suffix := range []string{".tsv", ".txt"} {
		if before, ok := strings.CutSuffix(orthoPrefix, suffix); ok {
			orthoPrefix = before
			break
		}
	}

	orthoSource := strings.TrimSuffix(orthoPrefix, "_orthology")

	lineCh, errCh := text.ReadLines(ctx, f)

	count := 0
	batch := make([]entity.Orthology, 0, uc.config.BatchSize)

	isFirstLine := true

	var species []string

	for {
		var err error

		line, ok := <-lineCh

		if !ok && len(batch) > 0 {
			err = uc.orthologyRepository.SaveMany(ctx, batch)
			if err != nil {
				return err
			}

			fmt.Println("Inserted last batch", len(batch), "orthologies... Total:", count)

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
			species := species[i-1]
			genes := strings.SplitSeq(strings.TrimSpace(cols[i]), ",")

			for gene := range genes {
				gene = strings.TrimSpace(gene)
				if gene == "" {
					continue
				}

				geneID := species + ":" + gene
				orthology.Orthologs = append(orthology.Orthologs, geneID)
			}
		}

		batch = append(batch, orthology)

		if len(batch) < uc.config.BatchSize {
			continue
		}

		err = uc.orthologyRepository.SaveMany(ctx, batch)
		if err != nil {
			return err
		}

		fmt.Println("Inserted", len(batch), "orthologies... Total:", count)

		batch = batch[:0] // reset batch len to 0, keep capacity
	}

	ctxCancel()

	if err := <-errCh; err != nil {
		return err
	}

	return nil
}
