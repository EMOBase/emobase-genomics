package version

import (
	"context"
	"errors"
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

// fakeUploads holds the assembly's current completed files and the Database
// Version's shared (orthology) ones.
type fakeUploads struct {
	IUploadFileRepository
	files  []entity.UploadFile
	shared []entity.UploadFile
}

func (u fakeUploads) ListCurrentCompletedSharedByVersionID(context.Context, uint64) ([]entity.UploadFile, error) {
	return u.shared, nil
}

func (u fakeUploads) ListCurrentCompletedByAssemblyVersionID(context.Context, uint64) ([]entity.UploadFile, error) {
	return u.files, nil
}

// fakeJobs reports every file's jobs as done except those of the files in
// nonDone, has nothing in flight, and records the jobs a release enqueues.
type fakeJobs struct {
	IJobRepository
	nonDone map[string]bool
	created []string
}

func (j *fakeJobs) HasNonDoneJobsForFile(_ context.Context, fileID string) (bool, error) {
	return j.nonDone[fileID], nil
}

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
			uc := New(fakeVersions{}, nil, jobs, fakeUploads{files: tc.files}, fakeAssemblies{}, nil, t.TempDir(), "")

			if _, err := uc.ReleaseVersion(context.Background(), "v1"); err != nil {
				t.Fatalf("release refused: %v", err)
			}
			if !slices.Equal(jobs.created, tc.want) {
				t.Errorf("enqueued %v, want %v", jobs.created, tc.want)
			}
		})
	}
}

// An assembly keeps every jbrowse.track (and species.synonym) it uploads, so
// one that failed must block the release even when a newer file of its type
// succeeded: otherwise the version is published with a track missing from
// JBrowse2 and nothing tells the admin. The failed track comes first because
// a check of one file per type would keep the newer, done one and miss it.
func TestReleaseVersion_RefusesFailedOlderFileOfMultiFileType(t *testing.T) {
	for _, ft := range []string{entity.FileTypeJBrowseTrack, entity.FileTypeSpeciesSynonym} {
		t.Run(ft, func(t *testing.T) {
			jobs := &fakeJobs{nonDone: map[string]bool{"old": true}}
			files := []entity.UploadFile{{ID: "old", FileType: ft}, {ID: "new", FileType: ft}}
			uc := New(fakeVersions{}, nil, jobs, fakeUploads{files: files}, fakeAssemblies{}, nil, t.TempDir(), "")

			_, err := uc.ReleaseVersion(context.Background(), "v1")
			if !errors.Is(err, ErrFileJobsNotComplete) {
				t.Fatalf("release error = %v, want ErrFileJobsNotComplete", err)
			}
			if len(jobs.created) != 0 {
				t.Errorf("refused release still enqueued %v", jobs.created)
			}
		})
	}
}

// Orthology is checked for the whole Database Version: every orthology.tsv,
// since a version keeps them all and a failed older one leaves its species
// pair's orthology out of the release, and the latest orthology.bundle, since
// a bundle that failed extracts nothing and only a newer bundle can clear it.
func TestReleaseVersion_RefusesFailedSharedOrthologyFile(t *testing.T) {
	cases := map[string][]entity.UploadFile{
		"failed older orthology.tsv": {
			{ID: "old", FileType: entity.FileTypeOrthologyTSV},
			{ID: "new", FileType: entity.FileTypeOrthologyTSV},
		},
		"failed latest orthology.bundle": {
			{ID: "old", FileType: entity.FileTypeOrthologyBundle},
		},
	}
	for name, shared := range cases {
		t.Run(name, func(t *testing.T) {
			jobs := &fakeJobs{nonDone: map[string]bool{"old": true}}
			uc := New(fakeVersions{}, nil, jobs, fakeUploads{shared: shared}, fakeAssemblies{}, nil, t.TempDir(), "")

			_, err := uc.ReleaseVersion(context.Background(), "v1")
			if !errors.Is(err, ErrFileJobsNotComplete) {
				t.Fatalf("release error = %v, want ErrFileJobsNotComplete", err)
			}
			if len(jobs.created) != 0 {
				t.Errorf("refused release still enqueued %v", jobs.created)
			}
		})
	}
}
