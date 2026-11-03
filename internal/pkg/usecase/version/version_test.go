package version

import (
	"context"
	"slices"
	"testing"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

type fakeVersions struct{ IVersionRepository }

func (fakeVersions) FindByName(_ context.Context, name string) (*entity.Version, error) {
	return &entity.Version{ID: 1, Name: name}, nil
}

type fakeAssemblies struct{ IAssemblyVersionRepository }

func (fakeAssemblies) ListByVersionID(_ context.Context, versionID uint64) ([]entity.AssemblyVersion, error) {
	return []entity.AssemblyVersion{{ID: 7, VersionID: versionID, SpeciesCode: "Tcas"}}, nil
}

// fakeUploads holds the assembly's latest completed files; the Database
// Version has no shared orthology files.
type fakeUploads struct {
	IUploadFileRepository
	files []entity.UploadFile
}

func (fakeUploads) FindLatestCompletedPerTypeByVersionID(context.Context, uint64) ([]entity.UploadFile, error) {
	return nil, nil
}

func (u fakeUploads) FindLatestCompletedPerTypeByAssemblyVersionID(context.Context, uint64) ([]entity.UploadFile, error) {
	return u.files, nil
}

// fakeJobs reports every file's jobs as done and nothing in flight, and records
// the jobs a release enqueues.
type fakeJobs struct {
	IJobRepository
	created []string
}

func (*fakeJobs) HasNonDoneJobsForFile(context.Context, string) (bool, error) { return false, nil }

func (*fakeJobs) HasActiveJobOfTypeForAssemblyVersion(context.Context, uint64, string) (bool, error) {
	return false, nil
}

func (j *fakeJobs) Create(_ context.Context, job *entity.Job) error {
	j.created = append(j.created, job.Type)
	return nil
}

// No file type is required to release: an assembly without genomic.fna must
// still release, and gets a REMOVE_BLAST job for the genome like any other
// absent type, so a genome database built by an earlier release of this
// assembly is not left behind and served.
func TestReleaseVersion_GenomicFNANotRequired(t *testing.T) {
	cases := map[string]struct {
		files []entity.UploadFile
		want  []string
	}{
		"no files at all": {
			want: []string{entity.JobTypeGenomicFNARemoveBlast, entity.JobTypeProteinFAARemoveBlast, entity.JobTypeRNAFNARemoveBlast},
		},
		"protein.faa only": {
			files: []entity.UploadFile{{ID: "p", FileType: entity.FileTypeProteinFAA}},
			want:  []string{entity.JobTypeGenomicFNARemoveBlast, entity.JobTypeProteinFAASetupBlast, entity.JobTypeRNAFNARemoveBlast},
		},
		"genomic.fna present": {
			files: []entity.UploadFile{{ID: "g", FileType: entity.FileTypeGenomicFNA}},
			want:  []string{entity.JobTypeGenomicFNASetupBlast, entity.JobTypeProteinFAARemoveBlast, entity.JobTypeRNAFNARemoveBlast},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			jobs := &fakeJobs{}
			uc := New(fakeVersions{}, nil, jobs, fakeUploads{files: tc.files}, fakeAssemblies{}, nil, t.TempDir())

			if _, err := uc.ReleaseVersion(context.Background(), "v1"); err != nil {
				t.Fatalf("release refused: %v", err)
			}
			if !slices.Equal(jobs.created, tc.want) {
				t.Errorf("enqueued %v, want %v", jobs.created, tc.want)
			}
		})
	}
}
