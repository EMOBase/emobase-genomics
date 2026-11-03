package synonym

import (
	"context"
	"strings"
	"testing"

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
