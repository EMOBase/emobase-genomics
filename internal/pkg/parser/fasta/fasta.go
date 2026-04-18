package fasta

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/EMOBase/emobase-genomics/internal/pkg/parser/text"
)

func ReadFastaRecords(ctx context.Context, f io.Reader) (<-chan FastaRecord, <-chan error) {
	recordCh := make(chan FastaRecord)
	errCh := make(chan error, 1)

	go func() {
		defer close(recordCh)
		defer close(errCh)

		textCh, textErrCh := text.ReadLines(ctx, f)
		var currentRecord *FastaRecord
		lineNum := 0

		for line := range textCh {
			lineNum++
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

				currentRecord = &FastaRecord{Line: lineNum, Header: header}
			} else if currentRecord != nil {
				currentRecord.Sequence += line
			} else {
				errCh <- fmt.Errorf("line %d: sequence data found before header", lineNum)
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
