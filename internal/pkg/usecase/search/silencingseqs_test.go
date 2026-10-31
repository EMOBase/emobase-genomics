package search

import (
	"context"
	"slices"
	"testing"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	"github.com/EMOBase/emobase-genomics/internal/pkg/usecase/versionresolver"
)

type fakeSynonymRepo struct{ docs []entity.Synonym }

func (f fakeSynonymRepo) FindBySynonymRelaxed(context.Context, string, string) ([]entity.Synonym, error) {
	return nil, nil
}
func (f fakeSynonymRepo) Suggest(context.Context, string, string) ([]string, error) { return nil, nil }
func (f fakeSynonymRepo) FindBySynonyms(_ context.Context, _ string, synonyms []string) ([]entity.Synonym, error) {
	var out []entity.Synonym
	for _, d := range f.docs {
		if slices.Contains(synonyms, d.Synonym) {
			out = append(out, d)
		}
	}
	return out, nil
}
func (f fakeSynonymRepo) FindByGenes(_ context.Context, _ string, genes []string) ([]entity.Synonym, error) {
	var out []entity.Synonym
	for _, d := range f.docs {
		if slices.Contains(genes, d.Gene) {
			out = append(out, d)
		}
	}
	return out, nil
}

type fakeDsRNARepo struct{ docs []entity.DsRNA }

func (f fakeDsRNARepo) FindByIDs(_ context.Context, _ string, ids []string) ([]entity.DsRNA, error) {
	var out []entity.DsRNA
	for _, d := range f.docs {
		if slices.Contains(ids, d.GetID()) {
			out = append(out, d)
		}
	}
	return out, nil
}

type fakeVersionRepo struct{}

func (fakeVersionRepo) FindByName(_ context.Context, name string) (*entity.Version, error) {
	return &entity.Version{ID: 1, Name: name}, nil
}
func (fakeVersionRepo) FindByID(context.Context, uint64) (*entity.Version, error) { return nil, nil }

type fakeAppSettings struct{}

func (fakeAppSettings) GetDefaultVersionID(context.Context) (*uint64, error) { return nil, nil }

// Every assembly's dsRNA and synonym indices share one alias per Version, so a
// lookup sees every species' records at once. Here Tcas and Dmel both have a
// dsRNA named "ds-1", with different sequences and genes.
func newSilencingSeqsUseCase() *UseCase {
	synonyms := fakeSynonymRepo{docs: []entity.Synonym{
		{Gene: "Tcas:TC016177", Type: entity.SYNONYM_TYPE_DSRNA, Synonym: "ds-1"},
		{Gene: "Dmel:FBgn0000001", Type: entity.SYNONYM_TYPE_DSRNA, Synonym: "ds-1"},
	}}
	dsrnas := fakeDsRNARepo{docs: []entity.DsRNA{
		{Gene: "Tcas:ds-1", Seq: "TCAS-SEQ"},
		{Gene: "Dmel:ds-1", Seq: "DMEL-SEQ"},
	}}
	resolver := versionresolver.New(fakeVersionRepo{}, fakeAppSettings{})
	return New(synonyms, nil, nil, nil, dsrnas, resolver, "emobase", "Tcas")
}

// A request for one species must return only that species' sequence and genes,
// even when another species uses the same dsRNA ID; the empty species of the
// deprecated GET /silencingseqs must still mean the configured main species.
func TestGetSilencingSeqs_ScopedToRequestedSpecies(t *testing.T) {
	uc := newSilencingSeqsUseCase()
	for _, tc := range []struct {
		name, species     string
		ids, geneIDs      []string
		wantSeq, wantGene string
	}{
		{"by id, non-main species", "Dmel", []string{"ds-1"}, nil, "DMEL-SEQ", "FBgn0000001"},
		{"by gene, non-main species", "Dmel", nil, []string{"FBgn0000001"}, "DMEL-SEQ", "FBgn0000001"},
		{"by id, main species", "Tcas", []string{"ds-1"}, nil, "TCAS-SEQ", "TC016177"},
		{"deprecated route, no species", "", []string{"ds-1"}, nil, "TCAS-SEQ", "TC016177"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := uc.GetSilencingSeqs(context.Background(), tc.species, tc.ids, tc.geneIDs, "v1")
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 {
				t.Fatalf("got %d records, want 1: %+v", len(got), got)
			}
			if got[0].ID != "ds-1" || got[0].Seq != tc.wantSeq {
				t.Errorf("got id %q seq %q, want ds-1 %q", got[0].ID, got[0].Seq, tc.wantSeq)
			}
			if !slices.Equal(got[0].GeneIDs, []string{tc.wantGene}) {
				t.Errorf("geneIds %v, want [%s] (no other species' genes, prefix stripped)", got[0].GeneIDs, tc.wantGene)
			}
		})
	}
}
