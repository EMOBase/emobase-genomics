package upload

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	"github.com/EMOBase/emobase-genomics/internal/pkg/uploadspec"
	"github.com/EMOBase/emobase-genomics/internal/pkg/usecase/worker/handlers"
	tusd "github.com/tus/tusd/v2/pkg/handler"
)

type fakeUploadFiles struct{ IUploadFileRepository }

func (fakeUploadFiles) UpdateStatus(context.Context, string, entity.UploadStatus) error { return nil }

// stageAndFinish stages content as tusd would, then runs the upload-finish hook.
func stageAndFinish(t *testing.T, uc *UseCase, id string, meta tusd.MetaData, content string) error {
	t.Helper()
	if err := os.WriteFile(filepath.Join(uc.uploadDir, id), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(uc.uploadDir, id+".info"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := uc.handlePreFinish(tusd.HookEvent{Context: context.Background(), Upload: tusd.FileInfo{ID: id, MetaData: meta}})
	return err
}

// finishUpload runs an upload through the finish hook and returns the path of
// the file it enqueued a job for.
func finishUpload(t *testing.T, uc *UseCase, jobs *fakeUploadJobs, id string, meta tusd.MetaData, content string) string {
	t.Helper()
	before := len(jobs.created)
	if err := stageAndFinish(t, uc, id, meta, content); err != nil {
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
		"_assemblyDir":       "Tribolium",
		"_displayLabel":      "v1 — Tribolium",
	}
	for k, v := range extra {
		meta[k] = v
	}
	return meta
}

// Files are stored under their uploaded name. Two tracks both named
// coverage.bw.gz are two files, so the second must not take the first's path:
// it would replace the first track's data, and deleting either track would
// delete both files. Normally refused when the upload starts; if both started
// before either finished, the second is refused when it finishes.
func TestUploadFinish_RefusesNameTakenMeanwhile(t *testing.T) {
	uc, jobs := newFinishTestUseCase(t)
	gz := "\x1f\x8b" // gzip magic, checked at upload finish
	meta := func(track string) tusd.MetaData {
		return assemblyMeta(entity.FileTypeJBrowseTrack, "coverage.bw.gz", map[string]string{"trackName": track})
	}

	first := finishUpload(t, uc, jobs, "aaaaaaaa000000000000000000000001", meta("Sample A"), gz+"first")
	if want := filepath.Join(uc.uploadDir, "v1", "Tribolium", "coverage.bw.gz"); first != want {
		t.Errorf("stored at %s, want %s", first, want)
	}

	second := "bbbbbbbb000000000000000000000002"
	err := stageAndFinish(t, uc, second, meta("Sample B"), gz+"second")
	requireStatus(t, err, http.StatusConflict, `this assembly already has a file named "coverage.bw.gz"`)
	if got, err := os.ReadFile(first); err != nil || string(got) != gz+"first" {
		t.Errorf("first track's file: got %q, %v; want it untouched", got, err)
	}
	if _, err := os.Stat(filepath.Join(uc.uploadDir, second)); !os.IsNotExist(err) {
		t.Errorf("refused upload's staged data should be removed: %v", err)
	}
}

// The same check runs when the upload starts, before any data is sent.
func TestPreUpload_RefusesNameOfAStoredFile(t *testing.T) {
	uc, _ := newFinishTestUseCase(t)
	store := func(rel string) {
		path := filepath.Join(uc.uploadDir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("\x1f\x8b"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// The fake assembly 7 has no folder name, so its files sit in the version folder.
	store("v1/coverage.bw.gz")
	store("v1/1.OrthoFinder_orthology.tsv.gz")
	track := tusd.MetaData{"fileType": entity.FileTypeJBrowseTrack, "fileName": "coverage.bw.gz", "version": "v1",
		"assembly": "7", "trackName": "Sample B"}
	orthology := tusd.MetaData{"fileType": entity.FileTypeOrthologyTSV, "fileName": "1.OrthoFinder_orthology.tsv.gz",
		"version": "v1", "order": "1", "algorithm": "OrthoFinder"}

	_, err := preUpload(uc, track)
	requireStatus(t, err, http.StatusConflict, `this assembly already has a file named "coverage.bw.gz"`)
	_, err = preUpload(uc, orthology)
	requireStatus(t, err, http.StatusConflict, `this version already has a file named "1.OrthoFinder_orthology.tsv.gz"`)

	track["fileName"] = "coverage_b.bw.gz"
	if _, err := preUpload(uc, track); err != nil {
		t.Errorf("upload under a free name refused: %v", err)
	}
}

// A bundle's archive is stored only while its job runs. A second bundle of the
// same name meanwhile is refused: the first job would extract and then delete
// the second's archive. Once the first job has ended, done or failed, its
// archive is gone and the name is free again; a failed bundle is fixed by
// uploading it again, so its archive must not block that.
func TestBundleUpload_NameFreeOnceItsJobEnds(t *testing.T) {
	uc, jobs := newFinishTestUseCase(t)
	meta := tusd.MetaData{"fileType": entity.FileTypeOrthologyBundle, "fileName": "orthology.tar.gz", "version": "v1"}
	finished := tusd.MetaData{"_versionID": "1"}
	maps.Copy(finished, meta)

	archive := finishUpload(t, uc, jobs, "aaaaaaaa000000000000000000000001", finished, "\x1f\x8bfirst")
	_, err := preUpload(uc, meta)
	requireStatus(t, err, http.StatusConflict, `a bundle named "orthology.tar.gz" is still being processed`)

	bundle := handlers.NewBundleHandler(uploadspec.Bundles[entity.FileTypeOrthologyBundle], nil, nil, nil)
	if err := bundle.OnFailure(context.Background(), *jobs.created[0], errors.New("bad manifest")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(archive); !os.IsNotExist(err) {
		t.Errorf("failed bundle's archive should be removed: %v", err)
	}
	if _, err := preUpload(uc, meta); err != nil {
		t.Errorf("re-upload of the failed bundle refused: %v", err)
	}
}
