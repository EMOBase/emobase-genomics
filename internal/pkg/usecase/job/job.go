package job

import (
	"context"
	"errors"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

var ErrJobNotFound = errors.New("job not found")

type UseCase struct {
	repo IJobRepository
}

func New(repo IJobRepository) *UseCase {
	return &UseCase{repo: repo}
}

func (uc *UseCase) GetJob(ctx context.Context, id uint64) (*entity.Job, error) {
	job, err := uc.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if job == nil {
		return nil, ErrJobNotFound
	}
	return job, nil
}
