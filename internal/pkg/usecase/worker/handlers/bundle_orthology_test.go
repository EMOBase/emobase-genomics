package handlers

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	"github.com/EMOBase/emobase-genomics/internal/pkg/jobpayload"
	"github.com/EMOBase/emobase-genomics/internal/pkg/uploadspec"
)

type fakeBundleFiles struct {
	rows    map[string]*entity.UploadFile
	created []*entity.UploadFile
}

func (f *fakeBundleFiles) FindByID(_ context.Context, id string) (*entity.UploadFile, error) {
	return f.rows[id], nil
}

func (f *fakeBundleFiles) Create(_ context.Context, u *entity.UploadFile) error {
	cp := *u
	f.rows[u.ID] = &cp
	f.created = append(f.created, &cp)
	return nil
}

func (f *fakeBundleFiles) UpdateStatus(_ context.Context, id string, s entity.UploadStatus) error {
	f.rows[id].UploadStatus = s
	return nil
}

type fakeBundleJobs struct {
	byFile  map[string]*entity.Job
	created []*entity.Job
}

func (f *fakeBundleJobs) Create(_ context.Context, j *entity.Job) error {
	j.ID = uint64(len(f.created) + 1)
	cp := *j
	f.created = append(f.created, &cp)
	if j.FileID != nil {
		f.byFile[*j.FileID+"/"+j.Type] = &cp
	}
	return nil
}

func (f *fakeBundleJobs) FindLatestByFileAndType(_ context.Context, fileID, jobType string) (*entity.Job, error) {
	return f.byFile[fileID+"/"+jobType], nil
}

type fakeBundleVersions struct{}

func (fakeBundleVersions) FindByID(_ context.Context, id uint64) (*entity.Version, error) {
	return &entity.Version{ID: id, Name: "v1"}, nil
}

// orthologyBundleFixture returns a handler and the bundle's job for an orthology
// bundle of two files. The bundle row has no assembly, as for every version-scoped upload.
func orthologyBundleFixture(t *testing.T) (*BundleHandler, *fakeBundleFiles, *fakeBundleJobs, entity.Job) {
	t.Helper()
	const bundleID = "bundle-1"
	archive := writeBundle(t,
		tarEntry{name: "manifest.csv", body: "fileName,order,algorithm\na.tsv,1,OrthoFinder\nb.tsv.gz,2,Eggnog\n"},
		tarEntry{name: "a.tsv", body: "group\tHsap:g1"},
		tarEntry{name: "b.tsv.gz", body: gzipped(t, "group\tMmus:g1")},
	)

	files := &fakeBundleFiles{rows: map[string]*entity.UploadFile{
		bundleID: {ID: bundleID, VersionID: 1, FileType: entity.FileTypeOrthologyBundle, CreatedBy: "alice"},
	}}
	jobs := &fakeBundleJobs{byFile: map[string]*entity.Job{}}
	h := NewBundleHandler(uploadspec.Bundles[entity.FileTypeOrthologyBundle], files, jobs, fakeBundleVersions{})

	raw, err := json.Marshal(jobpayload.ProcessPayload{UploadFileID: bundleID, VersionID: 1, FilePath: archive})
	if err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(raw)
	return h, files, jobs, entity.Job{Payload: &payload}
}

func TestBundleHandler_OrthologyChildrenCarryNoAssembly(t *testing.T) {
	h, files, jobs, job := orthologyBundleFixture(t)
	if _, err := h.Handle(context.Background(), job); err != nil {
		t.Fatal(err)
	}

	// Orthology belongs to the whole version. A child with an assembly would
	// drop out of the shared orthology status and release gate.
	if len(files.created) != 2 {
		t.Fatalf("created %d child files, want 2", len(files.created))
	}
	for _, f := range files.created {
		if f.FileType != entity.FileTypeOrthologyTSV {
			t.Errorf("child %s type %q, want %q", f.ID, f.FileType, entity.FileTypeOrthologyTSV)
		}
		if f.AssemblyVersionID != nil {
			t.Errorf("child %s has assembly %d, want none", f.ID, *f.AssemblyVersionID)
		}
		if f.CreatedBy != "alice" {
			t.Errorf("child %s created_by %q, want the bundle uploader", f.ID, f.CreatedBy)
		}
	}

	if len(jobs.created) != 2 {
		t.Fatalf("created %d child jobs, want 2", len(jobs.created))
	}
	for _, j := range jobs.created {
		if j.Type != entity.JobTypeOrthologyTSV {
			t.Errorf("job type %q, want %q", j.Type, entity.JobTypeOrthologyTSV)
		}
		if j.AssemblyVersionID != nil {
			t.Errorf("job %d has assembly %d, want none", j.ID, *j.AssemblyVersionID)
		}
	}

	// The gzipped child's content must match what the worker will read back.
	child := files.created[0]
	if got := readGzip(t, child.FilePath); got != "group\tHsap:g1" {
		t.Errorf("child %s content %q", child.ID, got)
	}
	if filepath.Dir(child.FilePath) != filepath.Dir(files.created[1].FilePath) {
		t.Error("children of one bundle must share the bundle's folder")
	}
}

// A requeued job (stuck-job recovery) must not duplicate children or jobs.
func TestBundleHandler_RerunCreatesNoDuplicates(t *testing.T) {
	h, files, jobs, job := orthologyBundleFixture(t)
	ctx := context.Background()
	if _, err := h.Handle(ctx, job); err != nil {
		t.Fatal(err)
	}
	firstFiles, firstJobs := len(files.created), len(jobs.created)

	if _, err := h.Handle(ctx, job); err != nil {
		t.Fatal(err)
	}
	if len(files.created) != firstFiles || len(jobs.created) != firstJobs {
		t.Errorf("rerun created %d files and %d jobs more, want none",
			len(files.created)-firstFiles, len(jobs.created)-firstJobs)
	}
}
