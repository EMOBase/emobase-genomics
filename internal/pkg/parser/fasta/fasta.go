package fasta

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/EMOBase/emobase-genomics/internal/pkg/parser/text"
)

// ReadFastaRecords streams parsed FASTA records from f, one per header entry.
func ReadFastaRecords(ctx context.Context, f io.Reader) (<-chan FastaRecord, <-chan error) {
	recordCh := make(chan FastaRecord)
	errCh := make(chan error, 1)

	go func() {
		defer close(recordCh)
		defer close(errCh)

		textCh, textErrCh := text.ReadLines(ctx, f)
		var currentHeader string
		var headerLine int
		var seqBuilder strings.Builder
		lineNum := 0
		inRecord := false

		flush := func() (FastaRecord, bool) {
			if !inRecord {
				return FastaRecord{}, false
			}
			r := FastaRecord{Line: headerLine, Header: currentHeader, Sequence: seqBuilder.String()}
			seqBuilder.Reset()
			inRecord = false
			return r, true
		}

		for line := range textCh {
			lineNum++
			if isEmptyLine(line) {
				continue
			}

			if isHeaderLine(line) {
				if r, ok := flush(); ok {
					select {
					case <-ctx.Done():
						errCh <- ctx.Err()
						return
					case recordCh <- r:
					}
				}
				headerLine = lineNum
				currentHeader = strings.SplitN(line[1:], " ", 2)[0]
				inRecord = true
			} else if inRecord {
				seqBuilder.WriteString(line)
			} else {
				errCh <- fmt.Errorf("line %d: sequence data found before header", lineNum)
				return
			}
		}

		if r, ok := flush(); ok {
			select {
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			case recordCh <- r:
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
