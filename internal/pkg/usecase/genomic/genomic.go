package genomic

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
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

	lineCh, errCh := readLines(ctx, f)

	count := 0
	batch := make([]entity.GenomicLocation, 0, uc.config.BatchSize)

	for line := range lineCh {
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

func readLines(ctx context.Context, f io.Reader) (<-chan string, <-chan error) {
	lineCh := make(chan string)
	errCh := make(chan error, 1)

	go func() {
		defer close(lineCh)
		defer close(errCh)

		reader := bufio.NewReader(f)

		for {
			line, err := reader.ReadString('\n')

			if err != nil {
				if err != io.EOF {
					errCh <- err
				} else {
					errCh <- nil
				}
				return
			}

			if len(line) > 0 {
				select {
				case <-ctx.Done():
					errCh <- ctx.Err()
					return
				case lineCh <- strings.TrimRight(line, "\n"):
				}
			}
		}
	}()

	return lineCh, errCh
}
