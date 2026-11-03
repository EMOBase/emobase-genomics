package search

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	"github.com/EMOBase/emobase-genomics/internal/pkg/indexname"
	"github.com/EMOBase/emobase-genomics/internal/pkg/usecase/versionresolver"
)

var (
	ErrVersionNotFound  = versionresolver.ErrVersionNotFound
	ErrNoDefaultVersion = versionresolver.ErrNoDefaultVersion
)

type GeneWithSynonyms struct {
	Gene     string   `json:"gene"`
	Synonyms []string `json:"synonyms"`
}

type GeneGroup struct {
	Species string             `json:"species"`
	Genes   []GeneWithSynonyms `json:"genes"`
}

type OrthologyResult struct {
	Group     string      `json:"group"`
	Source    string      `json:"source"`
	Orthologs []GeneGroup `json:"orthologs"`
}

type OtherGene struct {
	Species string `json:"species"`
	Gene    string `json:"gene"`
}

type SearchResult struct {
	Genes       []string          `json:"genes,omitempty"`
	Orthologies []OrthologyResult `json:"orthologies,omitempty"`
	OtherGenes  []OtherGene       `json:"otherGenes,omitempty"`
}

type UseCase struct {
	synonymRepo   ISynonymRepository
	orthologyRepo IOrthologyRepository
	sequenceRepo  ISequenceRepository
	genomicRepo   IGenomicLocationRepository
	dsrnaRepo     IDsRNARepository
	resolver      versionresolver.Resolver
	indexPrefix   string
}

func New(
	synonymRepo ISynonymRepository,
	orthologyRepo IOrthologyRepository,
	sequenceRepo ISequenceRepository,
	genomicRepo IGenomicLocationRepository,
	dsrnaRepo IDsRNARepository,
	resolver versionresolver.Resolver,
	indexPrefix string,
) *UseCase {
	return &UseCase{
		synonymRepo:   synonymRepo,
		orthologyRepo: orthologyRepo,
		sequenceRepo:  sequenceRepo,
		genomicRepo:   genomicRepo,
		dsrnaRepo:     dsrnaRepo,
		resolver:      resolver,
		indexPrefix:   indexPrefix,
	}
}

func (uc *UseCase) Suggest(ctx context.Context, prefix, versionName string) ([]string, error) {
	version, err := uc.resolver.Resolve(ctx, versionName)
	if err != nil {
		return nil, err
	}
	synonymIndex := fmt.Sprintf("%s-synonym-%s", uc.indexPrefix, indexname.FromVersionName(version.Name))
	return uc.synonymRepo.Suggest(ctx, synonymIndex, prefix)
}

// Search finds the genes whose synonyms match query. With a species, that
// species' matches come back as genes (IDs without the species prefix). With
// no species (a search of every species, none preferred), or when the species
// has no match, every match is another species' gene: it comes back in the
// orthology groups that contain it, or as otherGenes when none does.
func (uc *UseCase) Search(ctx context.Context, query, species, versionName string) (*SearchResult, error) {
	version, err := uc.resolver.Resolve(ctx, versionName)
	if err != nil {
		return nil, err
	}

	synonymIndex := fmt.Sprintf("%s-synonym-%s", uc.indexPrefix, indexname.FromVersionName(version.Name))
	orthologyIndex := fmt.Sprintf("%s-orthology-%s", uc.indexPrefix, indexname.FromVersionName(version.Name))

	if species != "" {
		speciesSynonyms, err := uc.synonymRepo.FindBySynonymRelaxed(ctx, synonymIndex, query, species)
		if err != nil {
			return nil, err
		}
		if len(speciesSynonyms) > 0 {
			prefix := species + ":"
			seen := make(map[string]bool)
			var genes []string
			for _, s := range speciesSynonyms {
				geneID := strings.TrimPrefix(s.Gene, prefix)
				if !seen[geneID] {
					seen[geneID] = true
					genes = append(genes, geneID)
				}
			}
			return &SearchResult{Genes: genes}, nil
		}
	}

	// The species, if any, has no match, so every match here is another species'.
	otherSynonyms, err := uc.synonymRepo.FindBySynonymRelaxed(ctx, synonymIndex, query, "")
	if err != nil {
		return nil, err
	}

	if len(otherSynonyms) == 0 {
		return &SearchResult{}, nil
	}

	geneToSynonyms := make(map[string][]entity.Synonym)
	for _, s := range otherSynonyms {
		geneToSynonyms[s.Gene] = append(geneToSynonyms[s.Gene], s)
	}

	otherGenes := make([]string, 0, len(geneToSynonyms))
	for gene := range geneToSynonyms {
		otherGenes = append(otherGenes, gene)
	}

	orthologies, err := uc.orthologyRepo.ListByOrthologs(ctx, orthologyIndex, otherGenes)
	if err != nil {
		return nil, err
	}

	if len(orthologies) > 0 {
		results := make([]OrthologyResult, len(orthologies))
		for i, o := range orthologies {
			results[i] = uc.enrichOrthology(o, geneToSynonyms)
		}
		return &SearchResult{Orthologies: results}, nil
	}

	otherGeneResults := make([]OtherGene, 0, len(otherGenes))
	for _, gene := range otherGenes {
		species, geneID := splitGeneID(gene)
		otherGeneResults = append(otherGeneResults, OtherGene{Species: species, Gene: geneID})
	}
	return &SearchResult{OtherGenes: otherGeneResults}, nil
}

func (uc *UseCase) enrichOrthology(o entity.Orthology, geneToSynonyms map[string][]entity.Synonym) OrthologyResult {
	speciesToGenes := make(map[string][]GeneWithSynonyms)
	for _, gene := range o.Orthologs {
		species, geneID := splitGeneID(gene)
		var synonymNames []string
		for _, s := range geneToSynonyms[gene] {
			if s.Type != entity.SYNONYM_TYPE_CURRENT_ID {
				synonymNames = append(synonymNames, s.Synonym)
			}
		}
		if synonymNames == nil {
			synonymNames = []string{}
		}
		speciesToGenes[species] = append(speciesToGenes[species], GeneWithSynonyms{
			Gene:     geneID,
			Synonyms: synonymNames,
		})
	}

	groups := make([]GeneGroup, 0, len(speciesToGenes))
	for species, genes := range speciesToGenes {
		groups = append(groups, GeneGroup{Species: species, Genes: genes})
	}
	// Species are fully symmetric now (no per-assembly "default"), so there is
	// no principled "main species first" anymore — order deterministically.
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Species < groups[j].Species
	})

	source, groupID := parseOrthologyGroup(o.Group)
	return OrthologyResult{
		Group:     groupID,
		Source:    source,
		Orthologs: groups,
	}
}

// SilencingSeq is the response shape for GET /silencingseqs/{species}.
type SilencingSeq struct {
	ID          string   `json:"id"`
	GeneIDs     []string `json:"geneIds"`
	Seq         string   `json:"seq"`
	LeftPrimer  string   `json:"leftPrimer,omitempty"`
	RightPrimer string   `json:"rightPrimer,omitempty"`
}

// deprecatedSilencingSeqsSpecies is the species of the deprecated
// GET /silencingseqs, which names none: the frontend still calling it reads Tcas.
const deprecatedSilencingSeqsSpecies = "Tcas"

// GetSilencingSeqs fetches one species' dsRNA records by dsRNA IDs (ids) or by
// gene IDs (geneIDs), both given without the species prefix. An empty species
// means Tcas, for the deprecated GET /silencingseqs.
// Exactly one of ids / geneIDs must be non-empty; the caller is responsible for
// enforcing mutual exclusivity before calling this method.
func (uc *UseCase) GetSilencingSeqs(ctx context.Context, species string, ids, geneIDs []string, versionName string) ([]SilencingSeq, error) {
	if species == "" {
		species = deprecatedSilencingSeqsSpecies
	}

	version, err := uc.resolver.Resolve(ctx, versionName)
	if err != nil {
		return nil, err
	}
	synonymIndex := fmt.Sprintf("%s-synonym-%s", uc.indexPrefix, indexname.FromVersionName(version.Name))
	dsrnaIndex := fmt.Sprintf("%s-dsrna-%s", uc.indexPrefix, indexname.FromVersionName(version.Name))
	prefix := species + ":"

	if len(ids) > 0 {
		return uc.getSilencingSeqsByIDs(ctx, prefix, ids, synonymIndex, dsrnaIndex)
	}
	if len(geneIDs) > 0 {
		return uc.getSilencingSeqsByGeneIDs(ctx, prefix, geneIDs, synonymIndex, dsrnaIndex)
	}
	return []SilencingSeq{}, nil
}

func (uc *UseCase) getSilencingSeqsByIDs(ctx context.Context, prefix string, ids []string, synonymIndex, dsrnaIndex string) ([]SilencingSeq, error) {
	allSynonyms, err := uc.synonymRepo.FindBySynonyms(ctx, synonymIndex, ids)
	if err != nil {
		return nil, err
	}
	// The synonym alias spans every species, and another species may use the
	// same dsRNA ID, so keep only this species' genes.
	synonymsByIB := make(map[string][]entity.Synonym)
	for _, s := range allSynonyms {
		if s.Type == entity.SYNONYM_TYPE_DSRNA && strings.HasPrefix(s.Gene, prefix) {
			synonymsByIB[s.Synonym] = append(synonymsByIB[s.Synonym], s)
		}
	}

	prefixed := make([]string, len(ids))
	for i, id := range ids {
		prefixed[i] = prefix + id
	}
	dsrnas, err := uc.dsrnaRepo.FindByIDs(ctx, dsrnaIndex, prefixed)
	if err != nil {
		return nil, err
	}

	return uc.buildSilencingSeqs(prefix, dsrnas, synonymsByIB), nil
}

func (uc *UseCase) getSilencingSeqsByGeneIDs(ctx context.Context, prefix string, geneIDs []string, synonymIndex, dsrnaIndex string) ([]SilencingSeq, error) {
	prefixedGenes := make([]string, len(geneIDs))
	for i, id := range geneIDs {
		prefixedGenes[i] = prefix + id
	}

	allSynonyms, err := uc.synonymRepo.FindByGenes(ctx, synonymIndex, prefixedGenes)
	if err != nil {
		return nil, err
	}
	synonymsByIB := make(map[string][]entity.Synonym)
	for _, s := range allSynonyms {
		if s.Type == entity.SYNONYM_TYPE_DSRNA {
			synonymsByIB[s.Synonym] = append(synonymsByIB[s.Synonym], s)
		}
	}
	if len(synonymsByIB) == 0 {
		return []SilencingSeq{}, nil
	}

	ibIDs := make([]string, 0, len(synonymsByIB))
	for ib := range synonymsByIB {
		ibIDs = append(ibIDs, ib)
	}
	prefixedIBs := make([]string, len(ibIDs))
	for i, ib := range ibIDs {
		prefixedIBs[i] = prefix + ib
	}

	dsrnas, err := uc.dsrnaRepo.FindByIDs(ctx, dsrnaIndex, prefixedIBs)
	if err != nil {
		return nil, err
	}

	return uc.buildSilencingSeqs(prefix, dsrnas, synonymsByIB), nil
}

func (uc *UseCase) buildSilencingSeqs(prefix string, dsrnas []entity.DsRNA, synonymsByIB map[string][]entity.Synonym) []SilencingSeq {
	result := make([]SilencingSeq, 0, len(dsrnas))
	for _, d := range dsrnas {
		rawID := strings.TrimPrefix(d.Gene, prefix)
		geneIDs := make([]string, 0)
		for _, s := range synonymsByIB[rawID] {
			geneIDs = append(geneIDs, strings.TrimPrefix(s.Gene, prefix))
		}
		result = append(result, SilencingSeq{
			ID:          rawID,
			GeneIDs:     geneIDs,
			Seq:         d.Seq,
			LeftPrimer:  d.LeftPrimer,
			RightPrimer: d.RightPrimer,
		})
	}
	return result
}

// splitGeneID splits "Species:GeneID" into its two parts.
func splitGeneID(gene string) (species, geneID string) {
	before, after, ok := strings.Cut(gene, ":")
	if !ok {
		return "", gene
	}
	return before, after
}

// parseOrthologyGroup parses "N.Algorithm:GroupID" into source and group ID.
// Example: "1.OrthoMCL:OG0001234" → source="OrthoMCL", groupID="OG0001234"
func parseOrthologyGroup(group string) (source, groupID string) {
	before, after, ok := strings.Cut(group, ":")
	if !ok {
		return "", group
	}
	prefix := before
	groupID = after
	_, after0, ok0 := strings.Cut(prefix, ".")
	if !ok0 {
		source = prefix
	} else {
		source = after0
	}
	return source, groupID
}
