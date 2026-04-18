package gff3

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/EMOBase/emobase-genomics/internal/pkg/parser/text"
	"github.com/samber/lo"
)

// TODO: Return errors with line number for better debugging
func ReadGFF3Records(ctx context.Context, f io.Reader) (<-chan GFF3Record, <-chan error) {
	lineCh := make(chan GFF3Record)
	errCh := make(chan error, 1)

	go func() {
		defer close(lineCh)
		defer close(errCh)

		textCh, textErrCh := text.ReadLines(ctx, f)
		lineNum := 0
		for line := range textCh {
			lineNum++
			if isHeaderLine(line) || isEmptyLine(line) {
				continue
			}

			gff3Record, err := parseLine(line)
			if err != nil {
				errCh <- fmt.Errorf("line %d: %w", lineNum, err)
				return
			}
			gff3Record.Line = lineNum

			select {
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			case lineCh <- gff3Record:
			}
		}

		if err := <-textErrCh; err != nil {
			errCh <- err
			return
		}
	}()

	return lineCh, errCh
}

func isHeaderLine(line string) bool {
	return strings.HasPrefix(line, "#")
}

func isEmptyLine(line string) bool {
	return line == ""
}

func parseLine(line string) (GFF3Record, error) {
	fields := strings.Split(line, delimiter)
	if len(fields) != 9 {
		return GFF3Record{}, ErrInvalidGFF3Line
	}

	if fields[0] == "" {
		return GFF3Record{}, ErrSeqIDMissing
	}

	if fields[2] == "" {
		return GFF3Record{}, ErrTypeMissing
	}

	if fields[3] == "" {
		return GFF3Record{}, ErrStartMissing
	}

	if fields[4] == "" {
		return GFF3Record{}, ErrEndMissing
	}

	if _, ok := symbolToStrand[fields[6]]; !ok {
		return GFF3Record{}, ErrInvalidStrand
	}

	start, err := strconv.Atoi(fields[3])
	if err != nil {
		return GFF3Record{}, err
	}

	end, err := strconv.Atoi(fields[4])
	if err != nil {
		return GFF3Record{}, err
	}

	attributes := parseAttributes(fields[8])

	return GFF3Record{
		SeqID:      fields[0],
		Source:     fields[1],
		Type:       fields[2],
		Start:      start,
		End:        end,
		Score:      fields[5],
		Strand:     fields[6],
		Phase:      fields[7],
		Attributes: attributes,
	}, nil
}

func parseAttributes(attrStr string) map[string][]string {
	attrs := strings.Split(attrStr, attributeDelimiter)

	res := make(map[string][]string)
	for _, kvStr := range attrs {
		kvStr = strings.TrimSpace(kvStr)

		parts := strings.SplitN(kvStr, keyValueSeparator, 2)
		if len(parts) != 2 || isEmptyValue(parts[1]) {
			continue
		}

		values := lo.Filter(strings.Split(parts[1], valueDelimiter), func(s string, _ int) bool {
			return !isEmptyValue(s)
		})

		res[parts[0]] = values
	}

	return res
}

func isEmptyValue(s string) bool {
	return s == ""
}
