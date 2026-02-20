package genomic

import (
	"context"
	"fmt"
	"io"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	"github.com/EMOBase/emobase-genomics/internal/pkg/parser/file"
	"github.com/EMOBase/emobase-genomics/internal/pkg/parser/gff3"
)

type GenomicLocationUseCase struct {
	config                    Config
	genomicLocationRepository IGenomicLocationRepository
}

func NewGenomicLocationUseCase(
	genomicLocationRepository IGenomicLocationRepository,
) *GenomicLocationUseCase {
	return &GenomicLocationUseCase{
		config:                    Config{BatchSize: 1000},
		genomicLocationRepository: genomicLocationRepository,
	}
}

func (uc *GenomicLocationUseCase) Load(ctx context.Context, f io.Reader) error {
	ctx, ctxCancel := context.WithCancel(ctx)
	defer ctxCancel()

	lineCh, errCh := file.ReadLines(ctx, f)

	count := 0
	batch := make([]entity.GenomicLocation, 0, uc.config.BatchSize)

	for {
		var err error

		line, ok := <-lineCh

		if !ok && len(batch) > 0 {
			err = uc.genomicLocationRepository.SaveMany(ctx, batch)
			if err != nil {
				return err
			}

			fmt.Println("Inserted last batch", len(batch), "genomic locations... Total:", count)

			break
		}

		count++

		if gff3.IsHeaderLine(line) || gff3.IsEmptyLine(line) {
			continue
		}

		gff3Record, err := gff3.ParseLine(line)
		if err != nil {
			return err
		}

		// filter(gff3Record -> Objects.equals("gene", gff3Record.getType()))
		if gff3Record.Type != "gene" {
			continue
		}

		loc, err := mapGFF3RecordToGenomicLocation(gff3Record)
		if err != nil {
			return err
		}

		batch = append(batch, loc)

		if len(batch) < uc.config.BatchSize {
			continue
		}

		err = uc.genomicLocationRepository.SaveMany(ctx, batch)
		if err != nil {
			return err
		}

		fmt.Println("Inserted", len(batch), "genomic locations... Total:", count)

		batch = batch[:0] // reset batch len to 0, keep capacity
	}

	ctxCancel()

	if err := <-errCh; err != nil {
		return err
	}

	return nil
}

func mapGFF3RecordToGenomicLocation(record gff3.GFF3Record) (entity.GenomicLocation, error) {
	gene, err := gff3.NCBIFindGeneID(record)
	if err != nil {
		return entity.GenomicLocation{}, err
	}

	loc := entity.GenomicLocation{
		Gene:         "Ptep:" + gene.Current, // TODO: Refactor to be like `species.createGeneId(gene)`
		ReferenceSeq: record.SeqID,
		Start:        record.Start,
		End:          record.End,
		Strand:       record.Strand,
	}

	return loc, nil
}
