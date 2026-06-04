package search

import (
	"context"
	"fmt"
	"strings"
)

type OrthologItem struct {
	Gene   string `json:"gene"`
	Source string `json:"source"`
}

type GeneOrthology struct {
	Gene      string         `json:"gene"`
	Orthologs []OrthologItem `json:"orthologs"`
}

// GetOrthologyBySpecies returns orthology groups for the given gene IDs (belonging
// to species), filtered by source algorithm. Each gene in the result is paired
// with its orthologs from all other species found in matching groups.
func (uc *UseCase) GetOrthologyBySpecies(ctx context.Context, species, genes, source, versionName string) ([]GeneOrthology, error) {
	version, err := uc.resolver.Resolve(ctx, versionName)
	if err != nil {
		return nil, err
	}
	orthologyIndex := fmt.Sprintf("%s-orthology-%s", uc.indexPrefix, strings.ToLower(version.Name))

	geneList := splitAndTrim(genes)
	queries := make([]string, len(geneList))
	for i, g := range geneList {
		queries[i] = species + ":" + g
	}

	orthologies, err := uc.orthologyRepo.ListByOrthologs(ctx, orthologyIndex, queries)
	if err != nil {
		return nil, err
	}

	grouped := make(map[string]*GeneOrthology)
	for _, o := range orthologies {
		orthoSource, _ := parseOrthologyGroup(o.Group)
		if !strings.EqualFold(source, "all") && !strings.EqualFold(source, orthoSource) {
			continue
		}
		for _, query := range queries {
			if !containsString(o.Orthologs, query) {
				continue
			}
			entry, ok := grouped[query]
			if !ok {
				_, geneID := splitGeneID(query)
				entry = &GeneOrthology{Gene: geneID, Orthologs: []OrthologItem{}}
				grouped[query] = entry
			}
			for _, olog := range o.Orthologs {
				oSpecies, oGeneID := splitGeneID(olog)
				if oSpecies == species {
					continue
				}
				entry.Orthologs = append(entry.Orthologs, OrthologItem{Gene: oGeneID, Source: orthoSource})
			}
		}
	}

	results := make([]GeneOrthology, 0, len(grouped))
	for _, v := range grouped {
		results = append(results, *v)
	}
	return results, nil
}

func splitAndTrim(s string) []string {
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			result = append(result, v)
		}
	}
	return result
}

func containsString(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
