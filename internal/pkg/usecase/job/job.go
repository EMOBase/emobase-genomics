package job

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

var ErrVersionNotFound = errors.New("version not found")

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
		summaries[i] = s
	}
	return summaries, nil
}
