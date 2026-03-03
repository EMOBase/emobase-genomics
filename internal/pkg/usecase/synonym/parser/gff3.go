package parser

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	"github.com/EMOBase/emobase-genomics/internal/pkg/parser/gff3"
)

var (
	ErrFirstRecordNotGene     = errors.New("first record is not a gene")
	ErrMRNAMustHaveGeneParent = errors.New("mRNA must have a gene parent declared before it")
	ErrCDSMustHaveMRNAParent  = errors.New("CDS must have an mRNA parent declared before it")
)

// TODO: Return errors with line number for better debugging
func ParseSynonyms(ctx context.Context, f io.Reader) (<-chan entity.Synonym, <-chan error) {
	synonymCh := make(chan entity.Synonym)
	errCh := make(chan error, 1)

	go func() {
		defer close(synonymCh)
		defer close(errCh)

		recordCh, recordErrCh := gff3.ReadGFF3Records(ctx, f)

		gff3Records := make([]gff3.GFF3Record, 0)
		for {
			gff3Record, ok := <-recordCh
			if !ok {
				if len(gff3Records) == 0 {
					break
				}

				synonyms, err := MakeSynonyms(gff3Records)
				if err != nil {
					errCh <- err
					return
				}

				for _, synonym := range synonyms {
					select {
					case <-ctx.Done():
						errCh <- ctx.Err()
						return
					case synonymCh <- synonym:
					}
				}

				break
			}

			if gff3Record.Type == "gene" {
				if len(gff3Records) == 0 {
					gff3Records = append(gff3Records, gff3Record)
				} else {
					synonyms, err := MakeSynonyms(gff3Records)
					if err != nil {
						errCh <- err
						return
					}

					for _, synonym := range synonyms {
						select {
						case <-ctx.Done():
							errCh <- ctx.Err()
							return
						case synonymCh <- synonym:
						}
					}

					gff3Records = []gff3.GFF3Record{gff3Record}
				}
			} else {
				if len(gff3Records) > 0 {
					gff3Records = append(gff3Records, gff3Record)
				}
			}
		}

		if err := <-recordErrCh; err != nil {
			errCh <- err
			return
		}
	}()

	return synonymCh, errCh
}

func MakeSynonyms(gff3Records []gff3.GFF3Record) ([]entity.Synonym, error) {
	synonyms := make([]entity.Synonym, 0)

	geneRecord := gff3Records[0]
	if geneRecord.Type != "gene" {
		return nil, ErrFirstRecordNotGene
	}

	geneXrefIdGroup, err := gff3.NCBIFindGeneID(geneRecord)
	if err != nil {
		return nil, err
	}

	species := "Ptep" // TODO: Use species from request input
	gene := species + ":" + geneXrefIdGroup.Current

	synonyms = append(synonyms, entity.Synonym{
		Gene:    gene,
		Type:    entity.SYNONYM_TYPE_OLD_ID,
		Synonym: geneXrefIdGroup.Current,
	})
	for _, previous := range geneXrefIdGroup.Previous {
		synonyms = append(synonyms, entity.Synonym{
			Gene:    gene,
			Type:    entity.SYNONYM_TYPE_OLD_ID,
			Synonym: previous,
		})
	}

	if name, ok := geneRecord.GetAttributeFirstValue("description"); ok {
		synonyms = append(synonyms, entity.Synonym{
			Gene:    gene,
			Type:    entity.SYNONYM_TYPE_NAME,
			Synonym: name,
		})
	}

	geneLocalId := geneRecord.GetID()
	mRNALocalIds := make(map[string]struct{})
	mRNAXrefIds := make(map[string]struct{})
	proteinXrefIds := make(map[string]struct{})

	for _, record := range gff3Records[1:] {
		switch record.Type {
		case "mRNA":
			mRNALocalIds[record.GetID()] = struct{}{}
			if geneLocalId == "" || geneLocalId != record.GetParentID() {
				return nil, ErrMRNAMustHaveGeneParent
			}

			if v, ok := record.GetAttributeFirstValue("transcript_id"); ok && v != "" {
				mRNAXrefIds[v] = struct{}{}
			}

			if v, ok := record.GetAttributeFirstValue("protein_id"); ok && v != "" {
				proteinIds := parseProteinId(v)
				for _, proteinId := range proteinIds {
					proteinXrefIds[proteinId] = struct{}{}
				}
			}

		case "CDS":
			if _, ok := mRNALocalIds[record.GetParentID()]; !ok {
				return nil, ErrCDSMustHaveMRNAParent
			}

			if v, ok := record.GetAttributeFirstValue("protein_id"); ok && v != "" {
				proteinIds := parseProteinId(v)
				for _, proteinId := range proteinIds {
					proteinXrefIds[proteinId] = struct{}{}
				}
			}

		default:
			// Ignore other types for now
		}
	}

	for mRNAXrefId := range mRNAXrefIds {
		synonyms = append(synonyms, entity.Synonym{
			Gene:    gene,
			Type:    entity.SYNONYM_TYPE_TRANSCRIPT,
			Synonym: mRNAXrefId,
		})
	}

	for proteinXrefId := range proteinXrefIds {
		synonyms = append(synonyms, entity.Synonym{
			Gene:    gene,
			Type:    entity.SYNONYM_TYPE_PROTEIN,
			Synonym: proteinXrefId,
		})
	}

	return synonyms, nil
}

func parseProteinId(input string) []string {
	vals := strings.Split(input, "|")

	var proteinIds []string
	var remains []string

	// Use index-based iteration to simulate queue (poll behavior)
	for i := 0; i < len(vals); {
		val := vals[i]
		i++

		switch val {
		case "gnl":
			if i+1 < len(vals) {
				proteinIds = append(proteinIds,
					val+"|"+vals[i]+"|"+vals[i+1])
				i += 2
			}
		case "gb":
			if i < len(vals) {
				proteinIds = append(proteinIds,
					val+"|"+vals[i])
				i++
			}
		default:
			remains = append(remains, val)
		}
	}

	if len(remains) > 0 {
		proteinIds = append(proteinIds, strings.Join(remains, "|"))
	}

	return proteinIds
}
