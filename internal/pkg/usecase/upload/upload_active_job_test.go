package upload

import (
	"context"
	"net/http"
	"slices"
	"testing"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	tusd "github.com/tus/tusd/v2/pkg/handler"
)

// busyFileTypes answers the active-job check from the file types of each
// assembly that have a pending or running job.
type busyFileTypes struct {
	*fakeUploadJobs
	busy map[uint64][]string
}

func (b busyFileTypes) HasActiveJobForFileTypeInAssemblyVersion(_ context.Context, asm uint64, fileType string) (bool, error) {
	return slices.Contains(b.busy[asm], fileType), nil
}

// The upload check asks whether any job still works on one of the assembly's
// files of the uploaded type, not whether a job type equal to the file type
// exists: a genomic.fna upload's only job is GENOMIC.FNA:SETUP_JBROWSE2, so
// asking by job type never caught one, and uploads of genomic.fna could pile up
// while one was still in flight. A busy file of another type must not block,
// and concurrent types (tracks) are never blocked.
func TestPreUpload_RejectsWhileAFileOfTheTypeHasAnActiveJob(t *testing.T) {
	uc, jobs := newTestUseCase()
	uc.jobRepo = busyFileTypes{fakeUploadJobs: jobs, busy: map[uint64][]string{
		7: {entity.FileTypeGenomicFNA, entity.FileTypeJBrowseTrack},
	}}
	meta := func(fileType, fileName string, extra map[string]string) tusd.MetaData {
		m := tusd.MetaData{"fileType": fileType, "fileName": fileName, "version": "v1", "assembly": "7"}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}

	_, err := preUpload(uc, meta(entity.FileTypeGenomicFNA, "genomic.fna.gz", nil))
	requireStatus(t, err, http.StatusConflict, `"genomic.fna" is already pending or running for this assembly`)

	if _, err := preUpload(uc, meta(entity.FileTypeRNAFNA, "rna.fna.gz", nil)); err != nil {
		t.Errorf("rna.fna upload blocked by another type's job: %v", err)
	}
	if _, err := preUpload(uc, meta(entity.FileTypeJBrowseTrack, "coverage.bw.gz", map[string]string{"trackName": "A"})); err != nil {
		t.Errorf("jbrowse.track upload blocked, but tracks may upload concurrently: %v", err)
	}
}
