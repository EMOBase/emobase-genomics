package assemblyversion

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

func (m *memAssemblies) Delete(context.Context, uint64) error { return nil }

type deleteJobs struct {
	IJobRepository
	created []entity.Job
}

func (*deleteJobs) HasActiveJobsByAssemblyVersionID(context.Context, uint64) (bool, error) {
	return false, nil
}
func (*deleteJobs) DeleteByAssemblyVersionID(context.Context, uint64) error { return nil }
func (j *deleteJobs) Create(_ context.Context, job *entity.Job) error {
	j.created = append(j.created, *job)
	return nil
}

type deleteUploads struct{ IUploadFileRepository }

func (deleteUploads) ListByAssemblyVersionID(context.Context, uint64) ([]entity.UploadFile, error) {
	return nil, nil
}
func (deleteUploads) HardDeleteByAssemblyVersionID(context.Context, uint64) error { return nil }

type deleteES struct{}

func (deleteES) DeleteIndexesByAssemblyVersion(context.Context, string, string) error { return nil }

type defaultVersion struct{ id uint64 }

func (d defaultVersion) GetDefaultVersionID(context.Context) (*uint64, error) { return &d.id, nil }

// Deleting an assembly removes its BLAST databases, but the BLAST server keeps
// serving them until it restarts, and only a worker can restart it. So when
// the assembly was in the default (served) version, a BLAST:RESTART job must
// be enqueued for that version. For another version, or an assembly that has
// no databases, nothing is served, and a restart would only cut off other
// users' running searches.
func TestDeleteAssemblyVersion_RemovesBlastDatabasesAndRestartsWhenServed(t *testing.T) {
	for name, tc := range map[string]struct {
		defaultID   uint64
		files       []string
		wantRestart bool
	}{
		"default version": {defaultID: 1, files: []string{"v1a1-genome.nsq", "v1a1-protein.psq"}, wantRestart: true},
		"other version":   {defaultID: 9, files: []string{"v1a1-genome.nsq"}},
		"no databases":    {defaultID: 1},
	} {
		t.Run(name, func(t *testing.T) {
			blastDir := t.TempDir()
			for _, f := range append(tc.files, "v1a2-genome.nsq") {
				if err := os.WriteFile(filepath.Join(blastDir, f), nil, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			assemblies := &memAssemblies{rows: []*entity.AssemblyVersion{{ID: 1, VersionID: 1, SpeciesCode: "Tcas"}}}
			jobs := &deleteJobs{}
			uc := New(fakeVersions{}, assemblies, jobs, deleteUploads{}, deleteES{}, defaultVersion{tc.defaultID}, t.TempDir(), blastDir)

			if err := uc.DeleteAssemblyVersion(context.Background(), "v1", 1); err != nil {
				t.Fatal(err)
			}

			entries, _ := os.ReadDir(blastDir)
			var left []string
			for _, e := range entries {
				left = append(left, e.Name())
			}
			if !slices.Equal(left, []string{"v1a2-genome.nsq"}) {
				t.Errorf("BLAST files left %v, want only the other assembly's [v1a2-genome.nsq]", left)
			}

			switch {
			case !tc.wantRestart && len(jobs.created) != 0:
				t.Errorf("enqueued %+v, want no BLAST restart", jobs.created)
			case tc.wantRestart && (len(jobs.created) != 1 || jobs.created[0].Type != entity.JobTypeBlastRestart ||
				jobs.created[0].VersionID != 1 || jobs.created[0].AssemblyVersionID != nil || jobs.created[0].FileID != nil):
				t.Errorf("enqueued %+v, want one BLAST:RESTART for version 1, with no assembly or file", jobs.created)
			}
		})
	}
}
