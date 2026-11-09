package search

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	"github.com/EMOBase/emobase-genomics/internal/pkg/usecase/versionresolver"
)

type fakeOrthologyRepo struct{ docs []entity.Orthology }

func (f fakeOrthologyRepo) ListByOrthologs(_ context.Context, _ string, genes []string) ([]entity.Orthology, error) {
	var out []entity.Orthology
	for _, o := range f.docs {
		if slices.ContainsFunc(o.Orthologs, func(g string) bool { return slices.Contains(genes, g) }) {
			out = append(out, o)
		}
	}
	return out, nil
}

// "vermilion" names a Tcas and a Dmel gene of one orthology group; "lsta-only"
// names a Lymnaea gene in no orthology group; "split" names the Tcas gene of the
// group and the Lymnaea gene in none.
func newSearchUseCase() *UseCase {
	synonyms := fakeSynonymRepo{docs: []entity.Synonym{
		{Gene: "Tcas:TC016177", Type: entity.SYNONYM_TYPE_SYMBOL, Synonym: "vermilion"},
		{Gene: "Dmel:FBgn0003996", Type: entity.SYNONYM_TYPE_SYMBOL, Synonym: "vermilion"},
		{Gene: "Lsta:GS0001", Type: entity.SYNONYM_TYPE_SYMBOL, Synonym: "lsta-only"},
		{Gene: "Tcas:TC016177", Type: entity.SYNONYM_TYPE_SYMBOL, Synonym: "split"},
		{Gene: "Lsta:GS0001", Type: entity.SYNONYM_TYPE_SYMBOL, Synonym: "split"},
	}}
	orthologies := fakeOrthologyRepo{docs: []entity.Orthology{
		{Group: "1.OrthoMCL:OG0001", Orthologs: []string{"Tcas:TC016177", "Dmel:FBgn0003996"}},
	}}
	resolver := versionresolver.New(fakeVersionRepo{}, fakeAppSettings{})
	return New(synonyms, orthologies, nil, nil, nil, resolver, "emobase")
}

// No species is "main" anymore. A species search returns that species' own
// matches as genes, whichever species it is. A search of every species (no
// species) prefers none, so even Tcas, the old main species, is not returned as
// genes: every match comes back in matchedGenes, and its orthology groups come
// back in orthologies. A species with no match falls back to the same view of
// the others. otherGenes keeps its narrower meaning: only matches that no
// returned orthology group contains.
func TestSearch_SpeciesMatchesAreGenes_OtherwiseOrthologyOrOtherGenes(t *testing.T) {
	uc := newSearchUseCase()
	for _, tc := range []struct {
		name, query, species string
		want                 string
	}{
		{"species search, Tcas", "vermilion", "Tcas", "genes [TC016177]"},
		{"species search, Dmel", "vermilion", "Dmel", "genes [FBgn0003996]"},
		{"every species: no genes, orthology", "vermilion", "", "matchedGenes [Dmel FBgn0003996, Tcas TC016177] + orthologies [OG0001: Dmel FBgn0003996, Tcas TC016177]"},
		{"species without a match: others' orthology", "vermilion", "Lsta", "matchedGenes [Dmel FBgn0003996, Tcas TC016177] + orthologies [OG0001: Dmel FBgn0003996, Tcas TC016177]"},
		{"every species, no orthology: otherGenes", "lsta-only", "", "matchedGenes [Lsta GS0001] + otherGenes [Lsta GS0001]"},
		{"every species, split: orthology and otherGenes", "split", "", "matchedGenes [Lsta GS0001, Tcas TC016177] + orthologies [OG0001: Dmel FBgn0003996, Tcas TC016177] + otherGenes [Lsta GS0001]"},
		{"no match anywhere", "nothing", "Tcas", "none"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := uc.Search(context.Background(), tc.query, tc.species, "v1")
			if err != nil {
				t.Fatal(err)
			}
			if s := describeSearchResult(got); s != tc.want {
				t.Errorf("Search(%q, species %q) = %s, want %s", tc.query, tc.species, s, tc.want)
			}
		})
	}
}

// describeSearchResult renders which field is set and its gene IDs, to compare
// whole results in one line.
func describeSearchResult(r *SearchResult) string {
	var parts []string
	if len(r.Genes) > 0 {
		parts = append(parts, "genes ["+strings.Join(r.Genes, " ")+"]")
	}
	if len(r.MatchedGenes) > 0 {
		var genes []string
		for _, g := range r.MatchedGenes {
			genes = append(genes, g.Species+" "+g.Gene)
		}
		parts = append(parts, "matchedGenes ["+strings.Join(genes, ", ")+"]")
	}
	if len(r.Orthologies) > 0 {
		var groups []string
		for _, o := range r.Orthologies {
			var genes []string
			for _, g := range o.Orthologs {
				for _, gene := range g.Genes {
					genes = append(genes, g.Species+" "+gene.Gene)
				}
			}
			groups = append(groups, o.Group+": "+strings.Join(genes, ", "))
		}
		parts = append(parts, "orthologies ["+strings.Join(groups, "; ")+"]")
	}
	if len(r.OtherGenes) > 0 {
		var genes []string
		for _, g := range r.OtherGenes {
			genes = append(genes, g.Species+" "+g.Gene)
		}
		parts = append(parts, "otherGenes ["+strings.Join(genes, ", ")+"]")
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, " + ")
}
