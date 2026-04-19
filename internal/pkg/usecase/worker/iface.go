package worker

import (
	"context"
	"time"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

// Handler processes a single job. Returning an error marks the job as failed
// (subject to retry). A nil return marks it as done.
type Handler interface {
	Handle(ctx context.Context, job entity.Job) error
}

type IJobRepository interface {
	ClaimNextPendingOfTypes(ctx context.Context, types []string) (*entity.Job, error)
	MarkDone(ctx context.Context, id uint64, resultMetadata []byte) error
	MarkFailed(ctx context.Context, id uint64, resultMetadata []byte) error
	RequeueStuckJobs(ctx context.Context, stuckBefore time.Time) (int64, error)
}
