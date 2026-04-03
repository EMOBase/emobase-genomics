package fasta

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/EMOBase/emobase-genomics/internal/pkg/parser/text"
)

var ErrInvalidFastaFormat = errors.New("invalid FASTA format: sequence data found before header")

func ReadFastaRecords(ctx context.Context, f io.Reader) (<-chan FastaRecord, <-chan error) {
	recordCh := make(chan FastaRecord)
	errCh := make(chan error, 1)

	go func() {
		defer close(recordCh)
		defer close(errCh)

		textCh, textErrCh := text.ReadLines(ctx, f)
		var currentRecord *FastaRecord

		for line := range textCh {
			if isEmptyLine(line) {
				continue
			}

			if isHeaderLine(line) {
				if currentRecord != nil {
					select {
					case <-ctx.Done():
						errCh <- ctx.Err()
						return
					case recordCh <- *currentRecord:
					}
				}

				header := line[1:]                     // Remove '>' from header
				header = strings.Split(header, " ")[0] // Keep only the first part of the header

				currentRecord = &FastaRecord{Header: header}
			} else if currentRecord != nil {
				currentRecord.Sequence += line
			} else {
				errCh <- ErrInvalidFastaFormat
				return
			}
		}

		if currentRecord != nil {
			select {
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			case recordCh <- *currentRecord:
			}
		}

		if err := <-textErrCh; err != nil {
			errCh <- err
			return
		}
	}()

	return recordCh, errCh
}

func isEmptyLine(line string) bool {
	return len(line) == 0
}

func isHeaderLine(line string) bool {
	return len(line) > 0 && line[0] == '>'
}
