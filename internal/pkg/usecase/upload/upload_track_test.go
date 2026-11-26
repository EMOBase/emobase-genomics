package upload

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	"github.com/EMOBase/emobase-genomics/internal/pkg/jobpayload"
	tusd "github.com/tus/tusd/v2/pkg/handler"
)

// A track upload is checked before any data is sent, so a selectInDefaultSession
// value the server cannot read fails the upload, instead of the track silently
// being left out of the default session.
func TestTrackUpload_RejectsUnreadableSelectInDefaultSession(t *testing.T) {
	uc, _ := newTestUseCase()
	_, err := preUpload(uc, tusd.MetaData{
		"fileType":               entity.FileTypeJBrowseTrack,
		"fileName":               "coverage.bw.gz",
		"version":                "v1",
		"assembly":               "7",
		"trackName":              "Coverage",
		"selectInDefaultSession": "yes",
	})
	requireStatus(t, err, http.StatusBadRequest, "selectInDefaultSession")
}

// A single upload must read the field like a bundle row does. This path once
// parsed it untrimmed, so a " true" here was read as false.
func TestTrackUpload_JobReadsTrimmedSelectInDefaultSession(t *testing.T) {
	uc, jobs := newTestUseCase()
	_, err := uc.enqueueProcessJob(context.Background(), "u1", tusd.MetaData{
		"fileType":               entity.FileTypeJBrowseTrack,
		"_versionID":             "1",
		"_assemblyVersionID":     "7",
		"trackName":              "Coverage",
		"selectInDefaultSession": " true",
	}, "/uploads/v1/Tribolium/coverage.bw.gz")
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs.created) != 1 || jobs.created[0].Type != entity.JobTypeJBrowseTrack {
		t.Fatalf("created %v, want one %s job", jobs.created, entity.JobTypeJBrowseTrack)
	}
	var p jobpayload.JBrowseTrackPayload
	if err := json.Unmarshal(*jobs.created[0].Payload, &p); err != nil {
		t.Fatal(err)
	}
	if !p.SelectInDefaultSession {
		t.Error(`selectInDefaultSession " true" was read as false`)
	}
}
