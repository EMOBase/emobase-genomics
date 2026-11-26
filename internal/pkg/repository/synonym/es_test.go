package synonym

import (
	"context"
	"strings"
	"testing"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	"github.com/EMOBase/emobase-genomics/internal/pkg/repository/estest"
)

// The synonyms of replaced GFF files are deleted by their file IDs only, from
// the index refreshed first, skipping conflicts so a synonym the new GFF just
// rewrote is never deleted under its old version. With no replaced file there
// is nothing to delete, and no request.
func TestDeleteByFileIDs_RefreshesThenDeletesThoseFilesOnly(t *testing.T) {
	client, rt := estest.New(t, nil)
	repo := New(client, 10)

	if err := repo.DeleteByFileIDs(context.Background(), "idx", nil); err != nil || len(rt.Requests) != 0 {
		t.Fatalf("no file IDs: err %v, requests %+v, want none", err, rt.Requests)
	}
	if err := repo.DeleteByFileIDs(context.Background(), "idx", []string{"gff-old"}); err != nil {
		t.Fatal(err)
	}
	if len(rt.Requests) != 2 || rt.Requests[0].Path != "/idx/_refresh" || rt.Requests[1].Path != "/idx/_delete_by_query" {
		t.Fatalf("requests %+v, want a refresh of idx, then a delete_by_query on idx", rt.Requests)
	}
	if !strings.Contains(rt.Requests[1].Query, "conflicts=proceed") {
		t.Errorf("delete_by_query params %q, want conflicts=proceed", rt.Requests[1].Query)
	}
	if want := `{"query":{"terms":{"file_id":["gff-old"]}}}`; strings.TrimSpace(rt.Requests[1].Body) != want {
		t.Errorf("body %s, want %s", rt.Requests[1].Body, want)
	}
}

// A species search must filter inside the ES query: a search returns at most
// 1000 synonyms, so filtering afterwards could find none of the species'
// matches once other species' fill that size. With no species, every
// species' synonyms are searched, as before.
func TestFindBySynonymRelaxed_FiltersSpeciesInTheQuery(t *testing.T) {
	for _, tc := range []struct{ species, want string }{
		{"Tcas", `{"query":{"bool":{"filter":{"prefix":{"gene":"Tcas:"}},"must":{"match":{"synonym":"vermilion"}}}},"size":1000}`},
		{"", `{"query":{"match":{"synonym":"vermilion"}},"size":1000}`},
	} {
		client, rt := estest.New(t, nil)
		if _, err := New(client, 10).FindBySynonymRelaxed(context.Background(), "idx", "vermilion", tc.species); err != nil {
			t.Fatal(err)
		}
		if len(rt.Requests) != 1 || rt.Requests[0].Path != "/idx/_search" {
			t.Fatalf("species %q: requests %+v, want one search of idx", tc.species, rt.Requests)
		}
		if got := strings.TrimSpace(rt.Requests[0].Body); got != tc.want {
			t.Errorf("species %q: body %s, want %s", tc.species, got, tc.want)
		}
	}
}

// A species-filtered suggestion reads synonym.suggest_species, whose only
// context is species_type: ES matches a suggestion having any one of the
// query's contexts, so species and type in two contexts would let every
// species' SYMBOLs through. Each type keeps its boost. Without a species the
// request is exactly the one sent before species existed.
func TestSuggest_SpeciesUsesTheCombinedContextElseUnchanged(t *testing.T) {
	for _, tc := range []struct{ species, want string }{
		{"", `{"size":0,"suggest":{"suggest":{"completion":{"contexts":{"synonym_type":[` +
			`{"boost":12,"context":"SYMBOL"},{"boost":8,"context":"NAME"},{"boost":4,"context":"CURRENT_ID"},` +
			`{"boost":4,"context":"DSRNA"},{"boost":4,"context":"OLD_ID"},{"boost":1,"context":"OTHER"}]},` +
			`"field":"synonym.suggest","size":100,"skip_duplicates":true},"prefix":"ver"}}}`},
		{"Tcas", `{"size":0,"suggest":{"suggest":{"completion":{"contexts":{"species_type":[` +
			`{"boost":12,"context":"Tcas|SYMBOL"},{"boost":8,"context":"Tcas|NAME"},{"boost":4,"context":"Tcas|CURRENT_ID"},` +
			`{"boost":4,"context":"Tcas|DSRNA"},{"boost":4,"context":"Tcas|OLD_ID"},{"boost":1,"context":"Tcas|OTHER"}]},` +
			`"field":"synonym.suggest_species","size":100,"skip_duplicates":true},"prefix":"ver"}}}`},
	} {
		client, rt := estest.New(t, nil)
		if _, err := New(client, 10).Suggest(context.Background(), "idx", "ver", tc.species); err != nil {
			t.Fatal(err)
		}
		if len(rt.Requests) != 1 {
			t.Fatalf("species %q: requests %+v, want one", tc.species, rt.Requests)
		}
		if got := strings.TrimSpace(rt.Requests[0].Body); got != tc.want {
			t.Errorf("species %q: body\n %s\nwant\n %s", tc.species, got, tc.want)
		}
	}
}

// Every stored synonym carries the species_type a species-filtered suggestion
// matches on, built from its gene's species prefix and its type, the same
// value es:migrate fills into older documents. The read-only Index is not stored.
func TestSaveMany_StoresSpeciesType(t *testing.T) {
	client, rt := estest.New(t, nil)
	err := New(client, 10).SaveMany(context.Background(), "idx", []entity.Synonym{
		{Gene: "Tcas:TC016177", Type: entity.SYNONYM_TYPE_SYMBOL, Synonym: "vermilion", FileID: "f1", Index: "ignored"},
		{Gene: "nospecies", Type: entity.SYNONYM_TYPE_NAME, Synonym: "x", FileID: "f1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(rt.Requests[0].Body), "\n")
	for i, want := range []string{
		`{"gene":"Tcas:TC016177","type":"SYMBOL","synonym":"vermilion","file_id":"f1","species_type":"Tcas|SYMBOL"}`,
		`{"gene":"nospecies","type":"NAME","synonym":"x","file_id":"f1","species_type":"|NAME"}`,
	} {
		if got := lines[2*i+1]; got != want {
			t.Errorf("document %d: %s, want %s", i, got, want)
		}
	}
}
