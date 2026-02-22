package sequence

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	"github.com/EMOBase/emobase-genomics/internal/pkg/parser/fasta"
)

type SequenceUseCase struct {
	config             Config
	sequenceRepository ISequenceRepository
}

func NewSequenceUseCase(
	sequenceRepository ISequenceRepository,
) *SequenceUseCase {
	return &SequenceUseCase{
		config:             Config{BatchSize: 1000},
		sequenceRepository: sequenceRepository,
	}
}

func (uc *SequenceUseCase) Load(ctx context.Context, f *os.File) error {
	ctx, ctxCancel := context.WithCancel(ctx)
	defer ctxCancel()

	sequenceType := determineSequenceType(f.Name())
	if sequenceType == SEQUENCE_TYPE_UNKNOWN {
		return ErrInvalidSequenceType
	}

	recordCh, errCh := fasta.ReadFastaRecords(ctx, f)
	count := 0
	batch := make([]entity.Sequence, 0, uc.config.BatchSize)

	for {
		var err error

		fastaRecord, ok := <-recordCh

		if !ok && len(batch) > 0 {
			err = uc.sequenceRepository.SaveMany(ctx, batch)
			if err != nil {
				return err
			}

			fmt.Println("Inserted last batch", len(batch), "sequences... Total:", count)

			break
		}

		count++

		sequence, err := mapFastaRecordToSequence(fastaRecord, sequenceType)
		if err != nil {
			return err
		}
		batch = append(batch, sequence)

		if len(batch) < uc.config.BatchSize {
			continue
		}

		err = uc.sequenceRepository.SaveMany(ctx, batch)
		if err != nil {
			return err
		}

		fmt.Println("Inserted", len(batch), "sequences... Total:", count)

		batch = batch[:0]
	}

	ctxCancel()

	if err := <-errCh; err != nil {
		return err
	}

	return nil
}

func determineSequenceType(fileName string) string {
	fileName = strings.ToLower(fileName)

	for _, suffix := range GZIP_EXTENSIONS {
		if strings.HasSuffix(fileName, suffix) {
			fileName = strings.TrimSuffix(fileName, suffix)
			break
		}
	}

	for _, suffix := range FASTA_EXTENSIONS {
		if strings.HasSuffix(fileName, suffix) {
			fileName = strings.TrimSuffix(fileName, suffix)
			break
		}
	}

	if fileNameHasSuffix(fileName, CDS_SUFFICES) {
		return SEQUENCE_TYPE_CDS
	} else if fileNameHasSuffix(fileName, TRANSCRIPT_SUFFICES) {
		return SEQUENCE_TYPE_TRANSCRIPT
	} else if fileNameHasSuffix(fileName, PROTEIN_SUFFICES) {
		return SEQUENCE_TYPE_PROTEIN
	}

	return SEQUENCE_TYPE_UNKNOWN
}

func fileNameHasSuffix(fileName string, suffixes []string) bool {
	for _, suffix := range suffixes {
		if strings.HasSuffix(fileName, suffix) {
			return true
		}
	}

	return false
}

func mapFastaRecordToSequence(record fasta.FastaRecord, sequenceType string) (entity.Sequence, error) {
	return entity.Sequence{
		Name:     record.Header,
		Sequence: record.Sequence,
		Type:     sequenceType,
		Species:  "Ptep", // TODO: Refactor this hardcoded value
	}, nil
}
