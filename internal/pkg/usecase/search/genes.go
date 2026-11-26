package search

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	"github.com/EMOBase/emobase-genomics/internal/pkg/indexname"
	ucsequence "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/sequence"
)

type GeneSequence struct {
	ID  string `json:"id"`
	Seq string `json:"seq"`
}

type GeneDetail struct {
	ID string `json:"id"`
	// The assembly this entry's data comes from, by its numeric ID and by its
	// opaque key (entity.AssemblyVersion.AssemblyID, also its JBrowse2 name).
	AssemblyVersionID uint64 `json:"assemblyVersionId"`
	AssemblyKey       string `json:"assemblyKey"`

	Symbol       string `json:"symbol,omitempty"`
	FullName     string `json:"fullname,omitempty"`
	AnnotationID string `json:"annotationId,omitempty"`

	Start   *int   `json:"start,omitempty"`
	End     *int   `json:"end,omitempty"`
	Strand  string `json:"strand,omitempty"`
	Seqname string `json:"seqname,omitempty"`

	MRNAs    []GeneSequence `json:"mRNAs,omitempty"`
	Proteins []GeneSequence `json:"proteins,omitempty"`
	CDS      []GeneSequence `json:"CDS,omitempty"`
}

// GetGenesBySpecies returns gene details for genes belonging to species matched by one of:
// ids (CURRENT_ID/OLD_ID lookup), symbol (SYMBOL), fullname (NAME), or annotationId (OTHER).
// Exactly one of the four params must be non-empty; the caller enforces this.
// Genomic location and sequence fields are populated only when that data exists in the index.
// A Version can hold several assemblies of one species with the same gene, so a gene
// gets one entry per assembly that has it, built from that assembly's documents only.
func (uc *UseCase) GetGenesBySpecies(ctx context.Context, species, ids, symbol, fullname, annotationID, versionName string) ([]GeneDetail, error) {
	version, err := uc.resolver.Resolve(ctx, versionName)
	if err != nil {
		return nil, err
	}

	versionLower := indexname.FromVersionName(version.Name)
	synonymIndex := fmt.Sprintf("%s-synonym-%s", uc.indexPrefix, versionLower)
	sequenceIndex := fmt.Sprintf("%s-sequence-%s", uc.indexPrefix, versionLower)
	genomicIndex := fmt.Sprintf("%s-genomiclocation-%s", uc.indexPrefix, versionLower)

	// Step 1: resolve input to full gene IDs via synonym lookup with type filter.
	lookupValues, typeFilter := resolveQueryParam(ids, symbol, fullname, annotationID)
	inputSynonyms, err := uc.synonymRepo.FindBySynonyms(ctx, synonymIndex, lookupValues)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	var geneIDs []string
	speciesPrefix := species + ":"
	for _, s := range inputSynonyms {
		if !typeFilter(s) {
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

	// Step 2: fetch all synonyms for those genes.
	geneSynonyms, err := uc.synonymRepo.FindByGenes(ctx, synonymIndex, geneIDs)
	if err != nil {
		return nil, err
	}

	// Every map below is keyed by assembly too: the aliases span every assembly
	// of the Version, and two assemblies' documents can share a gene or ID.
	geneSeqIDs := make(map[assemblyDoc][]string)
	geneSynonymMap := make(map[assemblyDoc][]entity.Synonym)
	geneAssemblies := make(map[string][]string) // gene → keys of the assemblies that have it
	var allSeqIDs []string
	for _, s := range geneSynonyms {
		assembly, err := assemblyOf(s.Index, synonymIndex)
		if err != nil {
			return nil, err
		}
		k := assemblyDoc{assembly: assembly, id: s.Gene}
		if _, ok := geneSynonymMap[k]; !ok {
			geneAssemblies[s.Gene] = append(geneAssemblies[s.Gene], assembly)
		}
		geneSynonymMap[k] = append(geneSynonymMap[k], s)
		switch s.Type {
		case entity.SYNONYM_TYPE_TRANSCRIPT:
			tID := seqGetID(species, ucsequence.SEQUENCE_TYPE_TRANSCRIPT, s.Synonym)
			cID := seqGetID(species, ucsequence.SEQUENCE_TYPE_CDS, s.Synonym)
			geneSeqIDs[k] = append(geneSeqIDs[k], tID, cID)
			allSeqIDs = append(allSeqIDs, tID, cID)
		case entity.SYNONYM_TYPE_PROTEIN:
			pID := seqGetID(species, ucsequence.SEQUENCE_TYPE_PROTEIN, s.Synonym)
			geneSeqIDs[k] = append(geneSeqIDs[k], pID)
			allSeqIDs = append(allSeqIDs, pID)
		}
	}

	// Step 3: fetch sequences and genomic locations (empty results are fine).
	sequences, err := uc.sequenceRepo.FindByIDs(ctx, sequenceIndex, allSeqIDs)
	if err != nil {
		return nil, err
	}

	locations, err := uc.genomicRepo.FindByIDs(ctx, genomicIndex, geneIDs)
	if err != nil {
		return nil, err
	}

	seqByID := make(map[assemblyDoc]entity.Sequence, len(sequences))
	for _, s := range sequences {
		assembly, err := assemblyOf(s.Index, sequenceIndex)
		if err != nil {
			return nil, err
		}
		seqByID[assemblyDoc{assembly: assembly, id: s.GetID()}] = s
	}
	locByGene := make(map[assemblyDoc]entity.GenomicLocation, len(locations))
	for _, loc := range locations {
		assembly, err := assemblyOf(loc.Index, genomicIndex)
		if err != nil {
			return nil, err
		}
		locByGene[assemblyDoc{assembly: assembly, id: loc.Gene}] = loc
	}

	// Build response: one GeneDetail per resolved gene ID and assembly that has
	// it, in gene order, then assembly ID order.
	results := make([]GeneDetail, 0, len(geneIDs))
	for _, fullGeneID := range geneIDs {
		_, geneID := splitGeneID(fullGeneID)
		assemblies := geneAssemblies[fullGeneID]
		slices.SortFunc(assemblies, func(a, b string) int {
			return cmp.Compare(assemblyVersionIDOf(a), assemblyVersionIDOf(b))
		})
		for _, assembly := range assemblies {
			k := assemblyDoc{assembly: assembly, id: fullGeneID}
			detail := GeneDetail{ID: geneID, AssemblyVersionID: assemblyVersionIDOf(assembly), AssemblyKey: assembly}

			for _, s := range geneSynonymMap[k] {
				switch s.Type {
				case entity.SYNONYM_TYPE_SYMBOL:
					detail.Symbol = s.Synonym
				case entity.SYNONYM_TYPE_NAME:
					detail.FullName = s.Synonym
				case entity.SYNONYM_TYPE_OTHER:
					detail.AnnotationID = s.Synonym
				}
			}

			if loc, ok := locByGene[k]; ok {
				start, end := loc.Start, loc.End
				detail.Start = &start
				detail.End = &end
				detail.Strand = loc.Strand
				detail.Seqname = loc.ReferenceSeq
			}

			for _, seqID := range geneSeqIDs[k] {
				s, ok := seqByID[assemblyDoc{assembly: assembly, id: seqID}]
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
	}
	return results, nil
}

// assemblyDoc identifies a document of one assembly: the same gene or sequence
// ID can belong to several assemblies of a Version.
type assemblyDoc struct{ assembly, id string }

// assemblyOf returns the assembly key of index, a concrete index behind alias.
// Every index behind the synonym, sequence and genomic aliases is a
// per-assembly one, so any other index is a bug, reported rather than skipped.
func assemblyOf(index, alias string) (string, error) {
	key, ok := indexname.AssemblyKey(index, alias)
	if !ok {
		return "", fmt.Errorf("index %q is not a per-assembly index of %q", index, alias)
	}
	return key, nil
}

// assemblyVersionIDOf returns the numeric Assembly Version ID in key, which
// assemblyOf has already validated.
func assemblyVersionIDOf(key string) uint64 {
	_, id, _ := entity.ParseAssemblyID(key)
	return id
}

// resolveQueryParam returns the lookup values and a synonym type filter based on
// which query param is set. Exactly one of ids/symbol/fullname/annotationID is expected
// to be non-empty; the caller is responsible for enforcing this.
func resolveQueryParam(ids, symbol, fullname, annotationID string) (values []string, filter func(entity.Synonym) bool) {
	switch {
	case ids != "":
		return splitAndTrim(ids), func(s entity.Synonym) bool {
			return s.Type == entity.SYNONYM_TYPE_CURRENT_ID || s.Type == entity.SYNONYM_TYPE_OLD_ID
		}
	case symbol != "":
		return []string{symbol}, func(s entity.Synonym) bool {
			return s.Type == entity.SYNONYM_TYPE_SYMBOL
		}
	case fullname != "":
		return []string{fullname}, func(s entity.Synonym) bool {
			return s.Type == entity.SYNONYM_TYPE_NAME
		}
	case annotationID != "":
		return []string{annotationID}, func(s entity.Synonym) bool {
			return s.Type == entity.SYNONYM_TYPE_OTHER && strings.HasPrefix(s.Synonym, "CG")
		}
	default:
		return nil, func(_ entity.Synonym) bool { return false }
	}
}

// seqGetID builds the ES document ID for a sequence: "species:type:name".
func seqGetID(species, seqType, name string) string {
	return species + ":" + seqType + ":" + name
}
