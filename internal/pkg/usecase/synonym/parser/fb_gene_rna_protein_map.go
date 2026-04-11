package parser

import (
	"bufio"
	"context"
	"io"
	"strings"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

// FlyBaseGeneRNAProteinMapParser parses FlyBase fbgn_fbtr_fbpp_*.tsv files.
// Each row has columns: gene_id, transcript_id, protein_id
// Produces CURRENT_ID, TRANSCRIPT, and PROTEIN synonyms.
type FlyBaseGeneRNAProteinMapParser struct {
	mainSpecies string
}

func NewFlyBaseGeneRNAProteinMapParser(mainSpecies string) *FlyBaseGeneRNAProteinMapParser {
	return &FlyBaseGeneRNAProteinMapParser{mainSpecies: mainSpecies}
}

func (p *FlyBaseGeneRNAProteinMapParser) Parse(ctx context.Context, r io.Reader) (<-chan entity.Synonym, <-chan error) {
	synonymCh := make(chan entity.Synonym)
	errCh := make(chan error, 1)

	go func() {
		defer close(synonymCh)
		defer close(errCh)

		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
				continue
			}

			cols := strings.Split(line, "\t")
			if len(cols) < 2 {
				continue
			}

			geneID := cols[0]
			gene := p.mainSpecies + ":" + geneID

			batch := []entity.Synonym{
				{Gene: gene, Type: entity.SYNONYM_TYPE_CURRENT_ID, Synonym: geneID},
			}

			if transcript := strings.TrimSpace(cols[1]); transcript != "" {
				batch = append(batch, entity.Synonym{Gene: gene, Type: entity.SYNONYM_TYPE_TRANSCRIPT, Synonym: transcript})
			}

			if len(cols) >= 3 {
				if protein := strings.TrimSpace(cols[2]); protein != "" {
					batch = append(batch, entity.Synonym{Gene: gene, Type: entity.SYNONYM_TYPE_PROTEIN, Synonym: protein})
				}
			}

			for _, s := range batch {
				select {
				case <-ctx.Done():
					errCh <- ctx.Err()
					return
				case synonymCh <- s:
				}
			}
		}

		if err := scanner.Err(); err != nil {
			errCh <- err
		}
	}()

	return synonymCh, errCh
}
