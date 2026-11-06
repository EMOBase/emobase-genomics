package dsrna

import (
	"context"
	"strings"
	"testing"

	"github.com/EMOBase/emobase-genomics/internal/pkg/repository/estest"
)

// After a re-upload, the assembly's older indexes are deleted. The wildcard
// "{alias}-v1a7-*" also matches a version named "v1-v1a7-x", whose index must
// survive, as must the live one.
func TestDeleteStaleIndexes_OnlyThisAssemblysOldIndexes(t *testing.T) {
	const live, stale, other = "p-dsrna-v1-v1a7-1700000002", "p-dsrna-v1-v1a7-1700000001", "p-dsrna-v1-v1a7-x-v9a1-1700000000"
	client, s := estest.New(t, func(r estest.Request) string {
		if r.Method == "GET" {
			return estest.IndexList(live, stale, other)
		}
		return ""
	})

	if err := New(client, 10).DeleteStaleIndexes(context.Background(), "p-dsrna-v1", live, "v1a7"); err != nil {
		t.Fatal(err)
	}
	var deletes []string
	for _, r := range s.Requests {
		if r.Method == "DELETE" {
			deletes = append(deletes, strings.TrimPrefix(r.Path, "/"))
		}
	}
	if len(deletes) != 1 || deletes[0] != stale {
		t.Errorf("deleted %v, want only the stale index [%s]", deletes, stale)
	}
}
