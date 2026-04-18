package genomic

import (
	"context"
	"fmt"
	"io"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	"github.com/EMOBase/emobase-genomics/internal/pkg/parser/gff3"
	"github.com/rs/zerolog/log"
)

type GenomicLocationUseCase struct {
	config Config
	repo   IGenomicLocationRepository
}

func New(
	repo IGenomicLocationRepository,
	mainSpecies string,
) *GenomicLocationUseCase {
	return &GenomicLocationUseCase{
		config: Config{MainSpecies: mainSpecies, BatchSize: 5000},
		repo:   repo,
	}
}

func (uc *GenomicLocationUseCase) Load(ctx context.Context, f io.Reader, indexName string) error {
	ctx, ctxCancel := context.WithCancel(ctx)
	defer ctxCancel()

	recordCh, errCh := gff3.ReadGFF3Records(ctx, f)

	count := 0
	batch := make([]entity.GenomicLocation, 0, uc.config.BatchSize)

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
		if record.Type != "gene" {
			continue
		}

		loc, err := uc.mapGFF3RecordToGenomicLocation(record)
		if err != nil {
			return fmt.Errorf("line %d: %w", record.Line, err)
		}

		batch = append(batch, loc)
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

	log.Ctx(ctx).Info().Int("total", count).Msg("genomic locations loaded")

	return nil
}

func (uc *GenomicLocationUseCase) mapGFF3RecordToGenomicLocation(record gff3.GFF3Record) (entity.GenomicLocation, error) {
	gene, err := gff3.NCBIFindGeneID(record)
	if err != nil {
		return entity.GenomicLocation{}, err
	}

	return entity.GenomicLocation{
		Gene:         uc.config.MainSpecies + ":" + gene.Current,
		ReferenceSeq: record.SeqID,
		Start:        record.Start,
		End:          record.End,
		Strand:       record.Strand,
	}, nil
}
