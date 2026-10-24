package job

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

var (
	ErrVersionNotFound = errors.New("version not found")
	ErrJobNotFound     = errors.New("job not found")
)

// JobSummary is the API-facing representation of a job for the list endpoint.
type JobSummary struct {
	ID          uint64           `json:"id"`
	VersionID   uint64           `json:"versionId"`
	FileID      *string          `json:"fileId,omitempty"`
	Type        string           `json:"type"`
	Description string           `json:"description"`
	Status      entity.JobStatus `json:"status"`
	Payload     *json.RawMessage `json:"payload"`
	Error       *string          `json:"error,omitempty"`
}

// JobDetail is the API-facing representation of a single job: its summary
// plus the assembly it belongs to and its lifecycle timestamps.
type JobDetail struct {
	JobSummary
	// AssemblyVersionID is nil for jobs shared across the whole version
	// (orthology).
	AssemblyVersionID *uint64    `json:"assemblyVersionId"`
	CreatedAt         time.Time  `json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
	StartedAt         *time.Time `json:"startedAt"`
	CompletedAt       *time.Time `json:"completedAt"`
}

type UseCase struct {
	repo        IJobRepository
	versionRepo IVersionRepository
}

func New(repo IJobRepository, versionRepo IVersionRepository) *UseCase {
	return &UseCase{repo: repo, versionRepo: versionRepo}
}

func (uc *UseCase) ListJobsByVersion(ctx context.Context, versionName string) ([]JobSummary, error) {
	v, err := uc.versionRepo.FindByName(ctx, versionName)
	if err != nil {
		return nil, err
	}
	if v == nil {
		return nil, ErrVersionNotFound
	}

	jobs, err := uc.repo.FindByVersionID(ctx, v.ID)
	if err != nil {
		return nil, err
	}

	summaries := make([]JobSummary, len(jobs))
	for i, j := range jobs {
		summaries[i] = toJobSummary(j)
	}
	return summaries, nil
}

// GetJob returns one job by its ID.
func (uc *UseCase) GetJob(ctx context.Context, id uint64) (*JobDetail, error) {
	j, err := uc.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if j == nil {
		return nil, ErrJobNotFound
	}

	return &JobDetail{
		JobSummary:        toJobSummary(*j),
		AssemblyVersionID: j.AssemblyVersionID,
		CreatedAt:         j.CreatedAt,
		UpdatedAt:         j.UpdatedAt,
		StartedAt:         j.StartedAt,
		CompletedAt:       j.CompletedAt,
	}, nil
}

func toJobSummary(j entity.Job) JobSummary {
	s := JobSummary{
		ID:          j.ID,
		VersionID:   j.VersionID,
		FileID:      j.FileID,
		Type:        j.Type,
		Description: j.Description,
		Status:      j.Status,
		Payload:     j.Payload,
	}
	if j.Status == entity.JobStatusFailed && j.ResultMetadata != nil {
		var meta struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(*j.ResultMetadata, &meta); err == nil && meta.Error != "" {
			s.Error = &meta.Error
		}
	}
	return s
}
