package upload

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	tusd "github.com/tus/tusd/v2/pkg/handler"
)

type statusRecorder struct {
	IUploadFileRepository
	statuses map[string]entity.UploadStatus
}

func (r *statusRecorder) UpdateStatus(_ context.Context, id string, s entity.UploadStatus) error {
	r.statuses[id] = s
	return nil
}

// When a complete upload can't be moved into place (here a plain file sits
// where the version's folder goes), its staging files must go. Otherwise tusd
// keeps reporting the upload as complete, the client's retry ends in success
// without sending anything, no job is created, and the portal waits for one
// forever. The error must be a 4xx: tus-js-client retries a 5xx, and since a
// retry re-sends bytes its retry count resets, so it would re-upload forever.
func TestUploadFinish_UnstorableUploadFailsVisiblyAndCleansUp(t *testing.T) {
	uc, jobs := newTestUseCase()
	uc.uploadDir = t.TempDir()
	statuses := &statusRecorder{statuses: map[string]entity.UploadStatus{}}
	uc.uploadRepo = statuses

	const id = "aaaaaaaa000000000000000000000001"
	staged, info := filepath.Join(uc.uploadDir, id), filepath.Join(uc.uploadDir, id+".info")
	for path, content := range map[string]string{staged: "\x1f\x8bbundle", info: "{}"} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(uc.uploadDir, "v1"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	meta := tusd.MetaData{"fileType": entity.FileTypeOrthologyBundle, "fileName": "bundle.tar.gz", "version": "v1", "_versionID": "1"}

	_, err := uc.handlePreFinish(tusd.HookEvent{Context: context.Background(), Upload: tusd.FileInfo{ID: id, MetaData: meta}})

	requireStatus(t, err, http.StatusUnprocessableEntity, "could not store the uploaded file")
	for _, path := range []string{staged, info} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s still exists (%v): a retry would see the upload as complete", filepath.Base(path), err)
		}
	}
	if statuses.statuses[id] != entity.UploadStatusFailed {
		t.Errorf("upload status %q, want FAILED", statuses.statuses[id])
	}
	if len(jobs.created) != 0 {
		t.Errorf("created %d jobs, want none", len(jobs.created))
	}
}
