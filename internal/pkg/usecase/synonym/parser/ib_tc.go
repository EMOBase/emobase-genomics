package parser

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

// IBTCParser parses iB-to-TC mapping CSV files (ib_tc_*.csv).
// Each data row has two comma-separated columns: iBID, TCgeneID.
// Produces DSRNA synonyms linking TC gene IDs to their iB identifiers.
type IBTCParser struct {
	species string
}

func NewIBTCParser(species string) *IBTCParser {
	return &IBTCParser{species: species}
}

func (p *IBTCParser) Parse(ctx context.Context, r io.Reader) (<-chan entity.Synonym, <-chan error) {
	synonymCh := make(chan entity.Synonym)
	errCh := make(chan error, 1)

	go func() {
		defer close(synonymCh)
		defer close(errCh)

		scanner := bufio.NewScanner(r)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()
			if strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
				continue
			}

			cols := strings.Split(line, ",")
			if len(cols) != 2 {
				errCh <- fmt.Errorf("line %d: iB/TC mapping file must have exactly 2 columns, got %d", lineNum, len(cols))
				return
			}

			ibID := strings.TrimSpace(cols[0])
			tcGeneID := strings.TrimSpace(cols[1])
			gene := p.species + ":" + tcGeneID

			select {
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			case synonymCh <- entity.Synonym{Gene: gene, Type: entity.SYNONYM_TYPE_DSRNA, Synonym: ibID}:
			}
		}

		if err := scanner.Err(); err != nil {
			errCh <- fmt.Errorf("line %d: %w", lineNum, err)
		}
	}()

	return synonymCh, errCh
}
