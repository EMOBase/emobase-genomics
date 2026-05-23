package search

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
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
	resolver      versionresolver.Resolver
	indexPrefix   string
	mainSpecies   string
}

func New(
	synonymRepo ISynonymRepository,
	orthologyRepo IOrthologyRepository,
	resolver versionresolver.Resolver,
	indexPrefix, mainSpecies string,
) *UseCase {
	return &UseCase{
		synonymRepo:   synonymRepo,
		orthologyRepo: orthologyRepo,
		resolver:      resolver,
		indexPrefix:   indexPrefix,
		mainSpecies:   mainSpecies,
	}
}

func (uc *UseCase) Suggest(ctx context.Context, prefix, versionName string) ([]string, error) {
	version, err := uc.resolver.Resolve(ctx, versionName)
	if err != nil {
		return nil, err
	}
	synonymIndex := fmt.Sprintf("%s-synonym-%s", uc.indexPrefix, strings.ToLower(version.Name))
	return uc.synonymRepo.Suggest(ctx, synonymIndex, prefix)
}

func (uc *UseCase) Search(ctx context.Context, query, versionName string) (*SearchResult, error) {
	version, err := uc.resolver.Resolve(ctx, versionName)
	if err != nil {
		return nil, err
	}

	synonymIndex := fmt.Sprintf("%s-synonym-%s", uc.indexPrefix, strings.ToLower(version.Name))
	orthologyIndex := fmt.Sprintf("%s-orthology-%s", uc.indexPrefix, strings.ToLower(version.Name))

	allSynonyms, err := uc.synonymRepo.FindBySynonymRelaxed(ctx, synonymIndex, query)
	if err != nil {
		return nil, err
	}

	mainPrefix := uc.mainSpecies + ":"
	var mainSynonyms, otherSynonyms []entity.Synonym
	for _, s := range allSynonyms {
		if strings.HasPrefix(s.Gene, mainPrefix) {
			mainSynonyms = append(mainSynonyms, s)
		} else {
			otherSynonyms = append(otherSynonyms, s)
		}
	}

	if len(mainSynonyms) > 0 {
		seen := make(map[string]bool)
		var genes []string
		for _, s := range mainSynonyms {
			geneID := strings.TrimPrefix(s.Gene, mainPrefix)
			if !seen[geneID] {
				seen[geneID] = true
				genes = append(genes, geneID)
			}
		}
		return &SearchResult{Genes: genes}, nil
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
	sort.Slice(groups, func(i, j int) bool {
		iIsMain := groups[i].Species == uc.mainSpecies
		jIsMain := groups[j].Species == uc.mainSpecies
		if iIsMain != jIsMain {
			return iIsMain
		}
		return groups[i].Species < groups[j].Species
	})

	source, groupID := parseOrthologyGroup(o.Group)
	return OrthologyResult{
		Group:     groupID,
		Source:    source,
		Orthologs: groups,
	}
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
