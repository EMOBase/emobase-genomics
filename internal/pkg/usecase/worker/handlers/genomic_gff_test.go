package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	"github.com/EMOBase/emobase-genomics/internal/pkg/jobpayload"
)

type fakeGenomicUC struct{ indexNames []string }

func (f *fakeGenomicUC) Load(_ context.Context, _ io.Reader, indexName, _, _ string, _, _ int, _ []string) error {
	f.indexNames = append(f.indexNames, indexName)
	return nil
}

type fakeGenomicRepo struct{ assemblyKeys []string }

func (f *fakeGenomicRepo) SetAlias(_ context.Context, _, _, assemblyKey string) error {
	f.assemblyKeys = append(f.assemblyKeys, assemblyKey)
	return nil
}

func (f *fakeGenomicRepo) DeleteStaleIndexes(context.Context, string, string, string) error {
	return nil
}

// Two assemblies of one species in one Database Version must not share an ES
// index. If they did, the second GFF upload would replace the first one's
// genomic locations, and deleting one assembly would take the other's data.
func TestGenomicGFF_SameSpeciesAssembliesGetSeparateIndexes(t *testing.T) {
	gff := filepath.Join(t.TempDir(), "genes.gff.gz")
	if err := os.WriteFile(gff, []byte(gzipped(t, "##gff-version 3")), 0644); err != nil {
		t.Fatal(err)
	}
	uc := &fakeGenomicUC{}
	repo := &fakeGenomicRepo{}
	h := NewGenomicGFFHandler(fakeBundleVersions{}, uc, repo, "emobasegenomics")

	for _, asmID := range []uint64{7, 8} {
		raw, err := json.Marshal(jobpayload.GenomicGFFPayload{
			UploadFileID:      fmt.Sprintf("file-%d", asmID),
			VersionID:         1,
			AssemblyVersionID: asmID,
			FilePath:          gff,
			Species:           "Tcas",
			GeneIDKey:         "locus_tag",
		})
		if err != nil {
			t.Fatal(err)
		}
		payload := json.RawMessage(raw)
		if _, err := h.Handle(context.Background(), entity.Job{Payload: &payload}); err != nil {
			t.Fatal(err)
		}
	}

	if len(repo.assemblyKeys) != 2 || repo.assemblyKeys[0] != "v1a7" || repo.assemblyKeys[1] != "v1a8" {
		t.Fatalf("alias keys = %v, want [v1a7 v1a8]", repo.assemblyKeys)
	}
	for i, key := range repo.assemblyKeys {
		if !strings.Contains(uc.indexNames[i], "-"+key+"-") {
			t.Errorf("index %q does not carry assembly key %q", uc.indexNames[i], key)
		}
	}
}
