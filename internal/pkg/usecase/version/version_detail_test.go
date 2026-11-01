package version

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

type fakeSettings struct{ IAppSettingsRepository }

func (fakeSettings) GetDefaultVersionID(context.Context) (*uint64, error) { return nil, nil }

// detailUploads and detailJobs leave ListByVersionID and FindByVersionID
// unimplemented: version detail must not load every file and job of the
// version (its orthology.tsv files can be many), so calling them panics.
type detailUploads struct {
	IUploadFileRepository
	shared, assembly []entity.UploadFile
}

func (u detailUploads) ListByVersionIDAndType(_ context.Context, _ uint64, fileType string, _, _ int) ([]entity.UploadFile, error) {
	var out []entity.UploadFile
	for _, f := range append(slices.Clone(u.shared), u.assembly...) {
		if f.FileType == fileType {
			out = append(out, f)
		}
	}
	return out, nil
}

func (u detailUploads) ListByAssemblyVersionID(context.Context, uint64) ([]entity.UploadFile, error) {
	return u.assembly, nil
}

func (detailUploads) TotalFileSizeByVersionIDs(context.Context, []uint64) (map[uint64]int64, error) {
	return map[uint64]int64{}, nil
}

type detailJobs struct {
	IJobRepository
	jobs []entity.Job
}

func (j detailJobs) FindByFileIDs(_ context.Context, fileIDs []string) ([]entity.Job, error) {
	var out []entity.Job
	for _, job := range j.jobs {
		if job.FileID != nil && slices.Contains(fileIDs, *job.FileID) {
			out = append(out, job)
		}
	}
	return out, nil
}

func (j detailJobs) FindByAssemblyVersionID(context.Context, uint64) ([]entity.Job, error) {
	var out []entity.Job
	for _, job := range j.jobs {
		if job.AssemblyVersionID != nil {
			out = append(out, job)
		}
	}
	return out, nil
}

func (detailJobs) StatusCountsByAssemblyVersionID(context.Context, uint64) (entity.JobStatusCounts, error) {
	return entity.JobStatusCounts{}, nil
}

func (detailJobs) StatusCountsForSharedJobs(context.Context, uint64) (entity.JobStatusCounts, error) {
	return entity.JobStatusCounts{}, nil
}

func fileIDs(files []FileDetail) []string {
	ids := []string{}
	for _, f := range files {
		ids = append(ids, f.ID)
	}
	return ids
}

// A bundle that fails validation extracts no files, yet its failed job turns
// the version or assembly status to ERROR. So version detail must list each
// bundle with its job and error, in its own bucket: an orthology bundle at the
// top level, a track bundle in its assembly's files. Bundles must not be mixed
// into the buckets of the files extracted from them, and orthology.tsv files
// are left to GET /versions/{name}/orthologies.
func TestGetVersionDetail_ListsBundlesWithTheirJobs(t *testing.T) {
	asm := uint64(7)
	fid := func(id string) *string { return &id }
	failed := json.RawMessage(`{"error":"manifest.csv: missing column \"order\""}`)

	uploads := detailUploads{
		shared: []entity.UploadFile{
			{ID: "ot1", FileType: entity.FileTypeOrthologyTSV},
			{ID: "ob1", FileType: entity.FileTypeOrthologyBundle},
		},
		assembly: []entity.UploadFile{
			{ID: "tb1", FileType: entity.FileTypeJBrowseTrackBundle, AssemblyVersionID: &asm},
			{ID: "t1", FileType: entity.FileTypeJBrowseTrack, AssemblyVersionID: &asm},
		},
	}
	jobs := detailJobs{jobs: []entity.Job{
		{ID: 1, FileID: fid("ot1"), Type: entity.JobTypeOrthologyTSV, Status: entity.JobStatusDone},
		{ID: 2, FileID: fid("ob1"), Type: entity.JobTypeOrthologyBundle, Status: entity.JobStatusFailed, ResultMetadata: &failed},
		{ID: 3, FileID: fid("tb1"), Type: entity.JobTypeJBrowseTrackBundle, Status: entity.JobStatusFailed, ResultMetadata: &failed, AssemblyVersionID: &asm},
		{ID: 4, FileID: fid("t1"), Type: entity.JobTypeJBrowseTrack, Status: entity.JobStatusDone, AssemblyVersionID: &asm},
	}}
	uc := New(fakeVersions{}, fakeSettings{}, jobs, uploads, fakeAssemblies{}, nil, t.TempDir(), "")

	d, err := uc.GetVersionDetail(context.Background(), "v1")
	if err != nil {
		t.Fatal(err)
	}
	files := d.Assemblies[0].Files

	for name, tc := range map[string]struct {
		got     []FileDetail
		wantIDs []string
		wantJob string
	}{
		"orthologyBundle":             {d.OrthologyBundle, []string{"ob1"}, entity.JobTypeOrthologyBundle},
		"files[jbrowse.track.bundle]": {files.JBrowseTrackBundle, []string{"tb1"}, entity.JobTypeJBrowseTrackBundle},
		"files[jbrowse.track]":        {files.JBrowseTrack, []string{"t1"}, entity.JobTypeJBrowseTrack},
	} {
		if !slices.Equal(fileIDs(tc.got), tc.wantIDs) {
			t.Errorf("%s: files %v, want %v", name, fileIDs(tc.got), tc.wantIDs)
			continue
		}
		if len(tc.got[0].Jobs) != 1 || tc.got[0].Jobs[0].Type != tc.wantJob {
			t.Errorf("%s: jobs %+v, want one %s", name, tc.got[0].Jobs, tc.wantJob)
		}
	}
	for name, bundle := range map[string][]FileDetail{"orthologyBundle": d.OrthologyBundle, "jbrowse.track.bundle": files.JBrowseTrackBundle} {
		if len(bundle) == 1 && len(bundle[0].Jobs) == 1 && bundle[0].Jobs[0].Error == nil {
			t.Errorf("%s: failed bundle job carries no error", name)
		}
	}
}
