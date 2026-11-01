package version

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

type deleteVersions struct{ fakeVersions }

func (deleteVersions) Delete(context.Context, uint64) error { return nil }

type deleteJobs struct{ IJobRepository }

func (deleteJobs) HasActiveJobsByVersionID(context.Context, uint64) (bool, error) { return false, nil }
func (deleteJobs) DeleteByVersionID(context.Context, uint64) error                { return nil }

type deleteUploads struct{ IUploadFileRepository }

func (deleteUploads) ListByVersionID(context.Context, uint64) ([]entity.UploadFile, error) {
	return nil, nil
}
func (deleteUploads) HardDeleteByVersionID(context.Context, uint64) error { return nil }

type deleteAssemblies struct{ IAssemblyVersionRepository }

func (deleteAssemblies) ListByVersionID(context.Context, uint64) ([]entity.AssemblyVersion, error) {
	return nil, nil
}
func (deleteAssemblies) DeleteByVersionID(context.Context, uint64) error { return nil }

type deleteES struct{}

func (deleteES) DeleteIndexesByVersion(context.Context, string, uint64) error { return nil }

// Only the default version's BLAST databases are served, and it cannot be
// deleted. But a release that failed part-way leaves its new databases
// behind, and the BLAST server would load them at its next restart. So
// deleting a version must remove all of its assemblies' databases, and no
// other version's.
func TestDeleteVersion_RemovesItsBlastDatabases(t *testing.T) {
	blastDir := t.TempDir()
	for _, f := range []string{"v1a7-genome.nsq", "v1a8-rna.nsq", "v12a7-genome.nsq"} {
		if err := os.WriteFile(filepath.Join(blastDir, f), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	uc := New(deleteVersions{}, fakeSettings{}, deleteJobs{}, deleteUploads{}, deleteAssemblies{}, deleteES{}, t.TempDir(), blastDir)

	if err := uc.DeleteVersion(context.Background(), "v1"); err != nil {
		t.Fatal(err)
	}

	entries, _ := os.ReadDir(blastDir)
	var left []string
	for _, e := range entries {
		left = append(left, e.Name())
	}
	if !slices.Equal(left, []string{"v12a7-genome.nsq"}) {
		t.Errorf("BLAST files left %v, want only version 12's [v12a7-genome.nsq]", left)
	}
}
