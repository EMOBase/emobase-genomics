package synonym

import (
	"context"
	"fmt"
	"os"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	"github.com/EMOBase/emobase-genomics/internal/pkg/parser/gff3"
	synonymGFF3Parser "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/synonym/parser"
)

type SynonymUseCase struct {
	config            Config
	synonymRepository ISynonymRepository
}

func NewSynonymUseCase(
	synonymRepository ISynonymRepository,
) *SynonymUseCase {
	return &SynonymUseCase{
		config:            Config{BatchSize: 1000},
		synonymRepository: synonymRepository,
	}
}

func (uc *SynonymUseCase) Load(ctx context.Context, f *os.File) error {
	ctx, ctxCancel := context.WithCancel(ctx)
	defer ctxCancel()

	// TODO: Move Synonym GFF3 parser to a separate package and use it here
	recordCh, errCh := gff3.ReadGFF3Records(ctx, f)
	count := 0
	batch := make([]entity.Synonym, 0, uc.config.BatchSize)

	gff3Records := make([]gff3.GFF3Record, 0)

	for {
		var err error

		gff3Record, ok := <-recordCh

		if !ok {
			if len(gff3Records) > 0 {
				synonyms, err := synonymGFF3Parser.MakeSynonyms(gff3Records)
				if err != nil {
					return err
				}
				for _, synonym := range synonyms {
					batch = append(batch, synonym)
					count++
				}
			}

			if len(batch) > 0 {
				err = uc.synonymRepository.SaveMany(ctx, batch)
				if err != nil {
					return err
				}

				fmt.Println("Inserted last batch", len(batch), "synonyms... Total:", count)
			}

			break
		}

		if gff3Record.Type == "gene" {
			if len(gff3Records) == 0 {
				gff3Records = append(gff3Records, gff3Record)
			} else {
				synonyms, err := synonymGFF3Parser.MakeSynonyms(gff3Records)
				if err != nil {
					return err
				}
				for _, synonym := range synonyms {
					batch = append(batch, synonym)
					count++
				}
				gff3Records = []gff3.GFF3Record{gff3Record}
			}
		} else {
			if len(gff3Records) > 0 {
				gff3Records = append(gff3Records, gff3Record)
			}
		}

		if len(batch) < uc.config.BatchSize {
			continue
		}

		err = uc.synonymRepository.SaveMany(ctx, batch)
		if err != nil {
			return err
		}

		fmt.Println("Inserted", len(batch), "synonyms... Total:", count)

		batch = batch[:0]
	}

	ctxCancel()

	if err := <-errCh; err != nil {
		return err
	}

	return nil
}
