package search

import (
	"context"
	"fmt"
	"strings"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	ucsequence "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/sequence"
)

type GeneSequence struct {
	ID  string `json:"id"`
	Seq string `json:"seq"`
}

type GeneDetail struct {
	ID       string         `json:"id"`
	Start    int            `json:"start"`
	End      int            `json:"end"`
	Strand   string         `json:"strand"`
	Seqname  string         `json:"seqname"`
	MRNAs    []GeneSequence `json:"mRNAs"`
	Proteins []GeneSequence `json:"proteins"`
	CDS      []GeneSequence `json:"CDS"`
}

// GetGenesBySpecies returns genomic location and associated sequences (mRNAs, proteins, CDS)
// for the given gene IDs belonging to species.
func (uc *UseCase) GetGenesBySpecies(ctx context.Context, species, ids, versionName string) ([]GeneDetail, error) {
	version, err := uc.resolver.Resolve(ctx, versionName)
	if err != nil {
		return nil, err
	}

	versionLower := strings.ToLower(version.Name)
	synonymIndex := fmt.Sprintf("%s-synonym-%s", uc.indexPrefix, versionLower)
	sequenceIndex := fmt.Sprintf("%s-sequence-%s", uc.indexPrefix, versionLower)
	genomicIndex := fmt.Sprintf("%s-genomiclocation-%s", uc.indexPrefix, versionLower)

	// Step 1: resolve input IDs to full gene IDs via exact synonym match.
	idList := splitAndTrim(ids)
	inputSynonyms, err := uc.synonymRepo.FindBySynonyms(ctx, synonymIndex, idList)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	var geneIDs []string // full IDs like "Tcas:TC016177"
	speciesPrefix := species + ":"
	for _, s := range inputSynonyms {
		if s.Type != entity.SYNONYM_TYPE_CURRENT_ID && s.Type != entity.SYNONYM_TYPE_OLD_ID {
			continue
		}
		if !strings.HasPrefix(s.Gene, speciesPrefix) {
			continue
		}
		if !seen[s.Gene] {
			seen[s.Gene] = true
			geneIDs = append(geneIDs, s.Gene)
		}
	}
	if len(geneIDs) == 0 {
		return []GeneDetail{}, nil
	}

	// Step 2: fetch all synonyms for those genes to find transcript/protein IDs.
	geneSynonyms, err := uc.synonymRepo.FindByGenes(ctx, synonymIndex, geneIDs)
	if err != nil {
		return nil, err
	}

	// Build map: gene → []sequenceID, collecting TRANSCRIPT+CDS and PROTEIN sequence IDs.
	geneSeqIDs := make(map[string][]string)
	var allSeqIDs []string
	for _, s := range geneSynonyms {
		switch s.Type {
		case entity.SYNONYM_TYPE_TRANSCRIPT:
			tID := seqGetID(species, ucsequence.SEQUENCE_TYPE_TRANSCRIPT, s.Synonym)
			cID := seqGetID(species, ucsequence.SEQUENCE_TYPE_CDS, s.Synonym)
			geneSeqIDs[s.Gene] = append(geneSeqIDs[s.Gene], tID, cID)
			allSeqIDs = append(allSeqIDs, tID, cID)
		case entity.SYNONYM_TYPE_PROTEIN:
			pID := seqGetID(species, ucsequence.SEQUENCE_TYPE_PROTEIN, s.Synonym)
			geneSeqIDs[s.Gene] = append(geneSeqIDs[s.Gene], pID)
			allSeqIDs = append(allSeqIDs, pID)
		}
	}

	// Step 3: fetch sequences and genomic locations in parallel via separate calls.
	sequences, err := uc.sequenceRepo.FindByIDs(ctx, sequenceIndex, allSeqIDs)
	if err != nil {
		return nil, err
	}

	locations, err := uc.genomicRepo.FindByIDs(ctx, genomicIndex, geneIDs)
	if err != nil {
		return nil, err
	}

	// Index sequences by their ES document ID for O(1) lookup.
	seqByID := make(map[string]entity.Sequence, len(sequences))
	for _, s := range sequences {
		seqByID[s.GetID()] = s
	}

	// Build response: one GeneDetail per genomic location.
	results := make([]GeneDetail, 0, len(locations))
	for _, loc := range locations {
		_, geneID := splitGeneID(loc.Gene)
		detail := GeneDetail{
			ID:       geneID,
			Start:    loc.Start,
			End:      loc.End,
			Strand:   loc.Strand,
			Seqname:  loc.ReferenceSeq,
			MRNAs:    []GeneSequence{},
			Proteins: []GeneSequence{},
			CDS:      []GeneSequence{},
		}

		for _, seqID := range geneSeqIDs[loc.Gene] {
			s, ok := seqByID[seqID]
			if !ok {
				continue
			}
			gs := GeneSequence{ID: s.Name, Seq: s.Sequence}
			switch s.Type {
			case ucsequence.SEQUENCE_TYPE_TRANSCRIPT:
				detail.MRNAs = append(detail.MRNAs, gs)
			case ucsequence.SEQUENCE_TYPE_PROTEIN:
				detail.Proteins = append(detail.Proteins, gs)
			case ucsequence.SEQUENCE_TYPE_CDS:
				detail.CDS = append(detail.CDS, gs)
			}
		}

		results = append(results, detail)
	}
	return results, nil
}

// seqGetID builds the ES document ID for a sequence: "species:type:name".
func seqGetID(species, seqType, name string) string {
	return species + ":" + seqType + ":" + name
}
