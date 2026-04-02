package genomic

import (
	"context"
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
		config: Config{MainSpecies: mainSpecies, BatchSize: 1000},
		repo:   repo,
	}
}

func (uc *GenomicLocationUseCase) Load(ctx context.Context, f io.Reader, indexName string) error {
	ctx, ctxCancel := context.WithCancel(ctx)
	defer ctxCancel()

	recordCh, errCh := gff3.ReadGFF3Records(ctx, f)

	count := 0
	batch := make([]entity.GenomicLocation, 0, uc.config.BatchSize)

	for {
		gff3Record, ok := <-recordCh

		if !ok {
			if len(batch) > 0 {
				if err := uc.repo.SaveMany(ctx, indexName, batch); err != nil {
					return err
				}
				count += len(batch)
				log.Ctx(ctx).Info().Int("batch", len(batch)).Int("total", count).Msg("inserted last batch of genomic locations")
			}
			break
		}

		if gff3Record.Type != "gene" {
			continue
		}

		loc, err := uc.mapGFF3RecordToGenomicLocation(gff3Record)
		if err != nil {
			return err
		}

		batch = append(batch, loc)

		if len(batch) < uc.config.BatchSize {
			continue
		}

		if err := uc.repo.SaveMany(ctx, indexName, batch); err != nil {
			return err
		}

		count += len(batch)
		log.Ctx(ctx).Info().Int("batch", len(batch)).Int("total", count).Msg("inserted batch of genomic locations")
		batch = batch[:0]
	}

	ctxCancel()

	if err := <-errCh; err != nil {
		return err
	}

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
