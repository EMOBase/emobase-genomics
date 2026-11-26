package handler

import (
	"cmp"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
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

func (r fakeJobRepo) FindByVersionID(_ context.Context, versionID uint64) ([]entity.Job, error) {
	var out []entity.Job
	for _, j := range r.jobs {
		if j.VersionID == versionID {
			out = append(out, j)
		}
	}
	slices.SortFunc(out, func(a, b entity.Job) int { return cmp.Compare(a.ID, b.ID) })
	return out, nil
}

type fakeJobVersionRepo struct{}

func (fakeJobVersionRepo) FindByName(_ context.Context, name string) (*entity.Version, error) {
	return &entity.Version{ID: 1, Name: name}, nil
}

func listJobIDs(t *testing.T, repo fakeJobRepo, path string) (int, []float64, map[string]any) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/jobs", NewJobHandler(ucjob.New(repo, fakeJobVersionRepo{})).ListByVersion)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %s", w.Body.String())
	}
	var ids []float64
	if data, ok := body["data"].([]any); ok {
		ids = []float64{}
		for _, item := range data {
			ids = append(ids, item.(map[string]any)["id"].(float64))
		}
	}
	return w.Code, ids, body
}

// A client lists one kind of a version's jobs (e.g. its BLAST setups after a
// release) by type: only jobs of that type may come back, and leaving the
// filter out must still list every job. A misspelled type must be a 400, not
// an empty list that reads as "no such jobs"; a real type with no jobs is [].
func TestJobHandlerListByVersion_FiltersByType(t *testing.T) {
	repo := fakeJobRepo{jobs: map[uint64]entity.Job{
		1: {ID: 1, VersionID: 1, Type: entity.JobTypeGenomicFNASetupBlast},
		2: {ID: 2, VersionID: 1, Type: entity.JobTypeGenomicGFF},
		3: {ID: 3, VersionID: 1, Type: entity.JobTypeGenomicFNASetupBlast},
	}}

	for _, tc := range []struct {
		name, path string
		wantIDs    []float64
	}{
		{"one type", "/jobs?version=v1&type=GENOMIC.FNA:SETUP_BLAST", []float64{1, 3}},
		{"no filter", "/jobs?version=v1", []float64{1, 2, 3}},
		{"type with no jobs", "/jobs?version=v1&type=DSRNA.CSV", []float64{}},
	} {
		code, ids, body := listJobIDs(t, repo, tc.path)
		if code != http.StatusOK || !slices.Equal(ids, tc.wantIDs) {
			t.Errorf("%s: got %d ids %v (body %v), want 200 ids %v", tc.name, code, ids, body, tc.wantIDs)
		}
	}

	if code, _, body := listJobIDs(t, repo, "/jobs?version=v1&type=genomic.fna:setup_blast"); code != http.StatusBadRequest || body["error"] != "invalid type filter" {
		t.Errorf("unknown type: got %d %v, want 400 invalid type filter", code, body)
	}
}
