package job

import (
	"context"
	"encoding/json"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

// JobSummary is the API-facing representation of a job for the list endpoint.
type JobSummary struct {
	ID        uint64           `json:"id"`
	VersionID uint64           `json:"versionId"`
	Type      string           `json:"type"`
	Status    entity.JobStatus `json:"status"`
	Payload   *json.RawMessage `json:"payload"`
	Error     *string          `json:"error,omitempty"`
}

type UseCase struct {
	repo IJobRepository
}

func New(repo IJobRepository) *UseCase {
	return &UseCase{repo: repo}
}

func (uc *UseCase) ListJobsByVersion(ctx context.Context, versionName string) ([]JobSummary, error) {
	jobs, err := uc.repo.FindByVersionName(ctx, versionName)
	if err != nil {
		return nil, err
	}

	summaries := make([]JobSummary, len(jobs))
	for i, j := range jobs {
		s := JobSummary{
			ID:        j.ID,
			VersionID: j.VersionID,
			Type:      j.Type,
			Status:    j.Status,
			Payload:   j.Payload,
		}
		if j.Status == entity.JobStatusFailed && j.ResultMetadata != nil {
			var meta struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(*j.ResultMetadata, &meta); err == nil && meta.Error != "" {
				s.Error = &meta.Error
			}
		}
		summaries[i] = s
	}
	return summaries, nil
}
