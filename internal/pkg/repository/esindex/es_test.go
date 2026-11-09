package esindex

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/EMOBase/emobase-genomics/internal/pkg/repository/estest"
)

// deleted returns the index names of the DELETE request, sorted, or nil.
func deleted(t *testing.T, s *estest.Server) []string {
	t.Helper()
	for _, r := range s.Requests {
		if r.Method == "DELETE" {
			names := strings.Split(strings.TrimPrefix(r.Path, "/"), ",")
			slices.Sort(names)
			return names
		}
	}
	return nil
}

// Version names may contain "-", so the wildcard for version "v1" also
// resolves to the indexes of versions "v1-test" and "v1-2". Deleting v1 must
// delete only v1's own indexes, of every type; the other versions' search
// data must survive.
func TestDeleteIndexesByVersion_OnlyThatVersionsIndexes(t *testing.T) {
	own := []string{
		"p-genomiclocation-v1-v1a7-1700000001",
		"p-orthology-v1-1700000000",
		"p-sequence-v1-v1a7-1700000000",
		"p-synonym-v1-v1a12-1700000000",
	}
	client, s := estest.New(t, func(r estest.Request) string {
		if r.Method == "GET" {
			return estest.IndexList(append([]string{
				"p-synonym-v1-test-v2a1-1700000000",
				"p-orthology-v1-2-1700000000",
				"p-dsrna-v1-test-v2a1-1700000005",
			}, own...)...)
		}
		return ""
	})

	if err := New(client, "p").DeleteIndexesByVersion(context.Background(), "v1", 1); err != nil {
		t.Fatal(err)
	}
	if got := deleted(t, s); !slices.Equal(got, own) {
		t.Errorf("deleted %v, want only version v1's %v", got, own)
	}
}

// Deleting one assembly resolves "{alias}-v1a7-*", which also matches a
// version named "v1-v1a7-x"; only assembly v1a7's own indexes may go, and
// with none of them left there must be no DELETE at all.
func TestDeleteIndexesByAssemblyVersion_OnlyThatAssemblysIndexes(t *testing.T) {
	list := []string{"p-synonym-v1-v1a7-x-v9a1-1700000000", "p-sequence-v1-v1a70-1700000000"}
	client, s := estest.New(t, func(r estest.Request) string {
		if r.Method == "GET" {
			return estest.IndexList(list...)
		}
		return ""
	})
	repo := New(client, "p")

	if err := repo.DeleteIndexesByAssemblyVersion(context.Background(), "v1", "v1a7"); err != nil {
		t.Fatal(err)
	}
	if got := deleted(t, s); got != nil {
		t.Fatalf("deleted %v, want no DELETE: none of these is assembly v1a7's", got)
	}

	list = append(list, "p-genomiclocation-v1-v1a7-1700000001", "p-synonym-v1-v1a7-1700000000")
	if err := repo.DeleteIndexesByAssemblyVersion(context.Background(), "v1", "v1a7"); err != nil {
		t.Fatal(err)
	}
	if got, want := deleted(t, s), []string{"p-genomiclocation-v1-v1a7-1700000001", "p-synonym-v1-v1a7-1700000000"}; !slices.Equal(got, want) {
		t.Errorf("deleted %v, want %v", got, want)
	}
}
