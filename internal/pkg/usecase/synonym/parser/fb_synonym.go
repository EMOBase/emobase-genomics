package parser

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

// FlyBaseSynonymParser parses FlyBase fb_synonym_*.tsv files.
// Each row has columns: gene_id, species, symbol, name, synonyms_pipe, more_synonyms_pipe
// Only rows for the configured species whose gene ID starts with "FBgn" are processed.
type FlyBaseSynonymParser struct {
	mainSpecies string
}

func NewFlyBaseSynonymParser(mainSpecies string) *FlyBaseSynonymParser {
	return &FlyBaseSynonymParser{mainSpecies: mainSpecies}
}

func (p *FlyBaseSynonymParser) Parse(ctx context.Context, r io.Reader) (<-chan entity.Synonym, <-chan error) {
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

			synonyms := p.parseLine(line)
			for _, s := range synonyms {
				if s.Synonym == "" || strings.Contains(s.Synonym, "::") {
					continue
				}
				select {
				case <-ctx.Done():
					errCh <- ctx.Err()
					return
				case synonymCh <- s:
				}
			}
		}

		if err := scanner.Err(); err != nil {
			errCh <- fmt.Errorf("line %d: %w", lineNum, err)
		}
	}()

	return synonymCh, errCh
}

func (p *FlyBaseSynonymParser) parseLine(line string) []entity.Synonym {
	cols := strings.Split(line, "\t")
	if len(cols) < 2 {
		return nil
	}

	geneID := cols[0]
	species := cols[1]

	// Only process rows for the configured species with FBgn gene IDs.
	if species != p.mainSpecies || !strings.HasPrefix(geneID, "FBgn") {
		return nil
	}

	gene := p.mainSpecies + ":" + geneID
	var synonyms []entity.Synonym
	seen := map[string]struct{}{}

	synonyms = append(synonyms, entity.Synonym{Gene: gene, Type: entity.SYNONYM_TYPE_CURRENT_ID, Synonym: geneID})

	if len(cols) > 2 && cols[2] != "" {
		synonyms = append(synonyms, entity.Synonym{Gene: gene, Type: entity.SYNONYM_TYPE_SYMBOL, Synonym: cols[2]})
		seen[normalizeSynonym(cols[2])] = struct{}{}
	}
	if len(cols) > 3 && cols[3] != "" {
		synonyms = append(synonyms, entity.Synonym{Gene: gene, Type: entity.SYNONYM_TYPE_NAME, Synonym: cols[3]})
		seen[normalizeSynonym(cols[3])] = struct{}{}
	}

	// Columns 4 and 5 hold pipe-separated additional synonyms.
	var others []string
	if len(cols) > 4 {
		others = append(others, strings.Split(cols[4], "|")...)
	}
	if len(cols) > 5 {
		others = append(others, strings.Split(cols[5], "|")...)
	}
	// Sort descending to match Java reference behaviour.
	sort.Sort(sort.Reverse(sort.StringSlice(others)))

	for _, other := range others {
		other = strings.TrimSpace(other)
		if other == "" {
			continue
		}
		norm := normalizeSynonym(other)
		if _, dup := seen[norm]; dup {
			continue
		}
		synonyms = append(synonyms, entity.Synonym{Gene: gene, Type: entity.SYNONYM_TYPE_OTHER, Synonym: other})
		seen[norm] = struct{}{}
	}

	return synonyms
}

func normalizeSynonym(s string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), "-", " "))
}
