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
