package handlers

import (
	"context"
	"encoding/json"
	"os"
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

// bundleCase describes one bundle upload for newBundleFixture.
type bundleCase struct {
	fileType          string
	assemblyVersionID *uint64 // nil for version-scoped bundles
	// archiveDir is the folder, relative to the temp root, the archive is stored
	// in. It mirrors where handlePreFinish puts the uploaded archive.
	archiveDir string
	entries    []tarEntry
}

const testBundleID = "bundle-1"

// newBundleFixture builds a BundleHandler for the given bundle type, with the
// archive stored under archiveDir, and returns the job the worker would run.
// The returned root is the temp root the archive lives under.
func newBundleFixture(t *testing.T, c bundleCase) (h *BundleHandler, files *fakeBundleFiles, jobs *fakeBundleJobs, job entity.Job, root string) {
	t.Helper()
	root = t.TempDir()
	dir := filepath.Join(root, c.archiveDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(dir, testBundleID+".tar.gz")
	if err := os.Rename(writeBundle(t, c.entries...), archive); err != nil {
		t.Fatal(err)
	}

	files = &fakeBundleFiles{rows: map[string]*entity.UploadFile{
		testBundleID: {
			ID:                testBundleID,
			VersionID:         1,
			AssemblyVersionID: c.assemblyVersionID,
			FileType:          c.fileType,
			CreatedBy:         "alice",
		},
	}}
	jobs = &fakeBundleJobs{byFile: map[string]*entity.Job{}}
	h = NewBundleHandler(uploadspec.Bundles[c.fileType], files, jobs, fakeBundleVersions{})

	raw, err := json.Marshal(jobpayload.ProcessPayload{UploadFileID: testBundleID, VersionID: 1, FilePath: archive})
	if err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(raw)
	return h, files, jobs, entity.Job{Payload: &payload}, root
}
