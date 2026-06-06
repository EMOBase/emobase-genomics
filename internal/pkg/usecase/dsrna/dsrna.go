package dsrna

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	"github.com/rs/zerolog/log"
)

type UseCase struct {
	repo        IDsRNARepository
	mainSpecies string
	batchSize   int
}

func New(repo IDsRNARepository, mainSpecies string, batchSize int) *UseCase {
	return &UseCase{repo: repo, mainSpecies: mainSpecies, batchSize: batchSize}
}

// Load parses a dsrna.csv (CSV format: id, seq, leftPrimer?, rightPrimer?) from r
// and bulk-indexes the records into indexName. Lines starting with '#' and blank
// lines are skipped. The gene ID is stored as "<mainSpecies>:<id>".
func (uc *UseCase) Load(ctx context.Context, r io.Reader, indexName string) error {
	scanner := bufio.NewScanner(r)

	count := 0
	batch := make([]entity.DsRNA, 0, uc.batchSize)

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := uc.repo.SaveMany(ctx, indexName, batch); err != nil {
			return err
		}
		count += len(batch)
		batch = batch[:0]
		return nil
	}

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		if strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
			continue
		}

		doc, err := uc.parseLine(line)
		if err != nil {
			return fmt.Errorf("line %d: %w", lineNum, err)
		}

		batch = append(batch, doc)
		if len(batch) >= uc.batchSize {
			if err := flush(); err != nil {
				return err
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed to read dsrna file: %w", err)
	}

	if err := flush(); err != nil {
		return err
	}

	log.Ctx(ctx).Info().Int("total", count).Msg("dsrna records loaded")
	return nil
}

func (uc *UseCase) parseLine(line string) (entity.DsRNA, error) {
	cols := strings.Split(line, ",")
	if len(cols) < 2 {
		return entity.DsRNA{}, fmt.Errorf("dsRNA CSV must have at least 2 columns, got %d", len(cols))
	}

	doc := entity.DsRNA{
		Gene: uc.mainSpecies + ":" + strings.TrimSpace(cols[0]),
		Seq:  strings.TrimSpace(cols[1]),
	}
	if len(cols) > 2 {
		doc.LeftPrimer = strings.TrimSpace(cols[2])
	}
	if len(cols) > 3 {
		doc.RightPrimer = strings.TrimSpace(cols[3])
	}
	return doc, nil
}
