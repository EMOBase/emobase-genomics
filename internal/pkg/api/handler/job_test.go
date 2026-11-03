package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	ucjob "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/job"
	"github.com/gin-gonic/gin"
)

type fakeJobRepo struct {
	ucjob.IJobRepository
	jobs map[uint64]entity.Job
}

func (r fakeJobRepo) FindByID(_ context.Context, id uint64) (*entity.Job, error) {
	j, ok := r.jobs[id]
	if !ok {
		return nil, nil
	}
	return &j, nil
}

func getJob(t *testing.T, repo fakeJobRepo, path string) (int, map[string]any) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/jobs/:id", NewJobHandler(ucjob.New(repo, nil)).Get)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %s", w.Body.String())
	}
	return w.Code, body
}

// A client polls GET /jobs/{id} with an id from the upload's X-Jobs header to
// learn whether the job finished and, if it failed, why. So a failed job must
// carry the worker's error message and its lifecycle timestamps, and a shared
// (orthology) job must report a null assembly rather than omit the field.
func TestJobHandlerGet_FailedJobReportsErrorAndTimestamps(t *testing.T) {
	started := time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)
	completed := started.Add(time.Minute)
	meta := json.RawMessage(`{"error":"failed to open file"}`)
	repo := fakeJobRepo{jobs: map[uint64]entity.Job{
		42: {
			ID: 42, VersionID: 1, Type: entity.JobTypeOrthologyTSV, Status: entity.JobStatusFailed,
			ResultMetadata: &meta, CreatedAt: started, UpdatedAt: completed, StartedAt: &started, CompletedAt: &completed,
		},
	}}

	code, body := getJob(t, repo, "/jobs/42")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %v", code, body)
	}
	data, _ := body["data"].(map[string]any)
	want := map[string]any{
		"id":          float64(42),
		"status":      string(entity.JobStatusFailed),
		"error":       "failed to open file",
		"startedAt":   started.Format(time.RFC3339),
		"completedAt": completed.Format(time.RFC3339),
	}
	for k, v := range want {
		if data[k] != v {
			t.Errorf("data.%s = %v, want %v", k, data[k], v)
		}
	}
	if v, ok := data["assemblyVersionId"]; !ok || v != nil {
		t.Errorf("data.assemblyVersionId = %v (present %v), want null", v, ok)
	}
}

// An unknown id is the caller's mistake, not a server error: 404, not a panic.
// A non-integer id cannot name any job: 400.
func TestJobHandlerGet_BadOrUnknownID(t *testing.T) {
	repo := fakeJobRepo{jobs: map[uint64]entity.Job{}}

	if code, body := getJob(t, repo, "/jobs/999"); code != http.StatusNotFound || body["error"] != "job not found" {
		t.Errorf("unknown id: got %d %v, want 404 job not found", code, body)
	}
	if code, body := getJob(t, repo, "/jobs/abc"); code != http.StatusBadRequest || body["error"] != "job id must be an integer" {
		t.Errorf("non-integer id: got %d %v, want 400", code, body)
	}
}
