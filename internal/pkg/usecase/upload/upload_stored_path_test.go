package upload

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	"github.com/EMOBase/emobase-genomics/internal/pkg/jobpayload"
	"github.com/EMOBase/emobase-genomics/internal/pkg/uploadspec"
	"github.com/EMOBase/emobase-genomics/internal/pkg/usecase/worker/handlers"
	tusd "github.com/tus/tusd/v2/pkg/handler"
)

type fakeUploadFiles struct{ IUploadFileRepository }

func (fakeUploadFiles) UpdateStatus(context.Context, string, entity.UploadStatus) error { return nil }

// finishUpload stages content as tusd would, then runs the upload-finish hook
// and returns the path of the file it enqueued a job for.
func finishUpload(t *testing.T, uc *UseCase, jobs *fakeUploadJobs, id string, meta tusd.MetaData, content string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(uc.uploadDir, id), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(uc.uploadDir, id+".info"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := len(jobs.created)
	if _, err := uc.handlePreFinish(tusd.HookEvent{Context: context.Background(), Upload: tusd.FileInfo{ID: id, MetaData: meta}}); err != nil {
		t.Fatalf("finish upload %s: %v", id, err)
	}
	if len(jobs.created) != before+1 {
		t.Fatalf("upload %s enqueued %d jobs, want 1", id, len(jobs.created)-before)
	}
	var p struct {
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal(*jobs.created[before].Payload, &p); err != nil {
		t.Fatal(err)
	}
	return p.FilePath
}

func newFinishTestUseCase(t *testing.T) (*UseCase, *fakeUploadJobs) {
	uc, jobs := newTestUseCase()
	uc.uploadDir = t.TempDir()
	uc.uploadRepo = fakeUploadFiles{}
	return uc, jobs
}

func assemblyMeta(fileType, fileName string, extra map[string]string) tusd.MetaData {
	meta := tusd.MetaData{
		"fileType":           fileType,
		"fileName":           fileName,
		"version":            "v1",
		"_versionID":         "1",
		"_assemblyVersionID": "7",
		"_assemblySpecies":   "Tcas",
		"_assemblyName":      "Tribolium",
		"_assemblyKey":       entity.FormatAssemblyID(1, 7),
	}
	for k, v := range extra {
		meta[k] = v
	}
	return meta
}

// Two uploads of one file name to one assembly (two tracks both named
// coverage.bw.gz, or a retried upload) used to be moved onto the same path, so
// the second silently replaced the first's file. Each must keep its own file,
// named after the original so its format is still recognized.
func TestUploadFinish_SameNameUploadsKeepSeparateFiles(t *testing.T) {
	uc, jobs := newFinishTestUseCase(t)
	gz := "\x1f\x8b" // gzip magic, checked at upload finish
	meta := func(track string) tusd.MetaData {
		return assemblyMeta(entity.FileTypeJBrowseTrack, "coverage.bw.gz", map[string]string{"trackName": track})
	}

	first := finishUpload(t, uc, jobs, "aaaaaaaa000000000000000000000001", meta("Sample A"), gz+"first")
	second := finishUpload(t, uc, jobs, "bbbbbbbb000000000000000000000002", meta("Sample B"), gz+"second")

	if first == second {
		t.Fatalf("both uploads stored at %s", first)
	}
	for path, want := range map[string]string{first: gz + "first", second: gz + "second"} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Errorf("%s: got %q, %v; want %q", path, got, err, want)
		}
	}
	if want := uploadspec.StoredFileName("coverage.bw.gz", "aaaaaaaa000000000000000000000001"); filepath.Base(first) != want {
		t.Errorf("stored name %q, want %q", filepath.Base(first), want)
	}
}

// A bundle job deletes its archive once it is done. With same-named bundles
// sharing a path, that deleted the archive of a later upload of the same name,
// whose own job then failed to open it.
func TestBundleCleanup_LeavesSameNamedBundleArchive(t *testing.T) {
	uc, jobs := newFinishTestUseCase(t)
	meta := assemblyMeta(entity.FileTypeJBrowseTrackBundle, "track_bundle.zip", nil)
	zip := "PK\x03\x04" // zip magic, checked at upload finish

	first := finishUpload(t, uc, jobs, "aaaaaaaa000000000000000000000001", meta, zip+"first")
	second := finishUpload(t, uc, jobs, "bbbbbbbb000000000000000000000002", meta, zip+"second")

	bundle := handlers.NewBundleHandler(uploadspec.Bundles[entity.FileTypeJBrowseTrackBundle], nil, nil, nil)
	if err := bundle.OnComplete(context.Background(), *jobs.created[0], nil); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(first); !os.IsNotExist(err) {
		t.Errorf("first bundle's archive should be removed by its own cleanup: %v", err)
	}
	if got, err := os.ReadFile(second); err != nil || string(got) != zip+"second" {
		t.Errorf("second bundle's archive: got %q, %v; want it untouched", got, err)
	}
	var p jobpayload.ProcessPayload
	if err := json.Unmarshal(*jobs.created[1].Payload, &p); err != nil || p.FilePath != second {
		t.Errorf("second job points at %q, want %q", p.FilePath, second)
	}
}
