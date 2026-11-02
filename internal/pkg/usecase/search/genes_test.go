package search

import (
	"context"
	"slices"
	"testing"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	ucsequence "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/sequence"
	"github.com/EMOBase/emobase-genomics/internal/pkg/usecase/versionresolver"
)

type fakeSequenceRepo struct{ docs []entity.Sequence }

func (f fakeSequenceRepo) FindByIDs(_ context.Context, _ string, ids []string) ([]entity.Sequence, error) {
	var out []entity.Sequence
	for _, d := range f.docs {
		if slices.Contains(ids, d.GetID()) {
			out = append(out, d)
		}
	}
	return out, nil
}

type fakeGenomicRepo struct{ docs []entity.GenomicLocation }

func (f fakeGenomicRepo) FindByIDs(_ context.Context, _ string, ids []string) ([]entity.GenomicLocation, error) {
	var out []entity.GenomicLocation
	for _, d := range f.docs {
		if slices.Contains(ids, d.Gene) {
			out = append(out, d)
		}
	}
	return out, nil
}

// Version "v1" (ID 1) holds two Tcas assemblies, 7 and 8, that both have gene
// TC000001, each with its own symbol, location and transcript. The aliases
// span both assemblies, so every lookup sees both assemblies' documents.
// Assembly 8's documents come first, as ES may return them in any order.
func newTwoAssemblyGenesUseCase(synonyms []entity.Synonym) *UseCase {
	const syn7, syn8 = "emobase-synonym-v1-v1a7-1700000000", "emobase-synonym-v1-v1a8-1700000000"
	const seq7, seq8 = "emobase-sequence-v1-v1a7-1700000000", "emobase-sequence-v1-v1a8-1700000000"
	if synonyms == nil {
		synonyms = []entity.Synonym{
			{Gene: "Tcas:TC000001", Type: entity.SYNONYM_TYPE_CURRENT_ID, Synonym: "TC000001", Index: syn8},
			{Gene: "Tcas:TC000001", Type: entity.SYNONYM_TYPE_SYMBOL, Synonym: "sym-8", Index: syn8},
			{Gene: "Tcas:TC000001", Type: entity.SYNONYM_TYPE_TRANSCRIPT, Synonym: "TC000001-RA", Index: syn8},
			{Gene: "Tcas:TC000001", Type: entity.SYNONYM_TYPE_CURRENT_ID, Synonym: "TC000001", Index: syn7},
			{Gene: "Tcas:TC000001", Type: entity.SYNONYM_TYPE_SYMBOL, Synonym: "sym-7", Index: syn7},
			{Gene: "Tcas:TC000001", Type: entity.SYNONYM_TYPE_TRANSCRIPT, Synonym: "TC000001-RA", Index: syn7},
		}
	}
	sequences := fakeSequenceRepo{docs: []entity.Sequence{
		{Species: "Tcas", Type: ucsequence.SEQUENCE_TYPE_TRANSCRIPT, Name: "TC000001-RA", Sequence: "SEQ-8", Index: seq8},
		{Species: "Tcas", Type: ucsequence.SEQUENCE_TYPE_TRANSCRIPT, Name: "TC000001-RA", Sequence: "SEQ-7", Index: seq7},
	}}
	locations := fakeGenomicRepo{docs: []entity.GenomicLocation{
		{Gene: "Tcas:TC000001", ReferenceSeq: "chr-8", Start: 800, End: 899, Index: "emobase-genomiclocation-v1-v1a8-1700000000"},
		{Gene: "Tcas:TC000001", ReferenceSeq: "chr-7", Start: 700, End: 799, Index: "emobase-genomiclocation-v1-v1a7-1700000000"},
	}}
	resolver := versionresolver.New(fakeVersionRepo{}, fakeAppSettings{})
	return New(fakeSynonymRepo{docs: synonyms}, nil, sequences, locations, nil, resolver, "emobase", "Tcas")
}

// A gene that two assemblies of one species both have is reported once per
// assembly, each entry naming its assembly (for the caller to tell them apart
// and open the right JBrowse2 assembly) and built from that assembly's data
// only: never one assembly's symbol with the other's location, and never the
// same transcript twice.
func TestGetGenesBySpecies_OneEntryPerAssemblyWithItsOwnData(t *testing.T) {
	got, err := newTwoAssemblyGenesUseCase(nil).GetGenesBySpecies(context.Background(), "Tcas", "TC000001", "", "", "", "v1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2 (one per assembly): %+v", len(got), got)
	}
	for i, want := range []struct {
		assemblyVersionID   uint64
		assemblyKey, symbol string
		seqname, mRNA       string
		start               int
	}{
		{7, "v1a7", "sym-7", "chr-7", "SEQ-7", 700},
		{8, "v1a8", "sym-8", "chr-8", "SEQ-8", 800},
	} {
		g := got[i]
		if g.ID != "TC000001" || g.AssemblyVersionID != want.assemblyVersionID || g.AssemblyKey != want.assemblyKey {
			t.Errorf("entry %d: id %q assembly %d/%q, want TC000001 %d/%q (in assembly ID order)",
				i, g.ID, g.AssemblyVersionID, g.AssemblyKey, want.assemblyVersionID, want.assemblyKey)
		}
		if g.Symbol != want.symbol || g.Seqname != want.seqname || g.Start == nil || *g.Start != want.start {
			t.Errorf("entry %d (%s): symbol %q seqname %q start %v, want %q %q %d",
				i, want.assemblyKey, g.Symbol, g.Seqname, g.Start, want.symbol, want.seqname, want.start)
		}
		if len(g.MRNAs) != 1 || g.MRNAs[0].Seq != want.mRNA {
			t.Errorf("entry %d (%s): mRNAs %+v, want exactly [%s]", i, want.assemblyKey, g.MRNAs, want.mRNA)
		}
	}
}

// Every index behind the synonym alias is a per-assembly one. A document from
// any other index can't be attributed to an assembly, so the lookup fails
// rather than silently dropping the gene or reporting it without its assembly.
func TestGetGenesBySpecies_FailsOnIndexWithoutAssembly(t *testing.T) {
	uc := newTwoAssemblyGenesUseCase([]entity.Synonym{
		{Gene: "Tcas:TC000001", Type: entity.SYNONYM_TYPE_CURRENT_ID, Synonym: "TC000001", Index: "emobase-synonym-v1-1700000000"},
	})
	if got, err := uc.GetGenesBySpecies(context.Background(), "Tcas", "TC000001", "", "", "", "v1"); err == nil {
		t.Fatalf("got %+v and no error, want an error naming the index", got)
	}
}
