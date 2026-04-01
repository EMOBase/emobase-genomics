package worker

import (
	"context"
	"time"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

type IJobRepository interface {
	ClaimNextPending(ctx context.Context) (*entity.Job, error)
	MarkDone(ctx context.Context, id uint64, resultMetadata []byte) error
	MarkFailed(ctx context.Context, id uint64, resultMetadata []byte) error
	RequeueStuckJobs(ctx context.Context, stuckBefore time.Time) (int64, error)
}
