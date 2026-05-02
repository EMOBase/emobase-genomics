package worker

import (
	"context"
	"encoding/json"
	"time"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

// Handler processes a single job. Returning an error marks the job as failed.
// A nil return marks it as done. The returned json.RawMessage is persisted as
// ResultMetadata and forwarded to OnCompleteHook if the handler implements it.
type Handler interface {
	Handle(ctx context.Context, job entity.Job) (json.RawMessage, error)
}

type IJobRepository interface {
	ClaimNextPending(ctx context.Context) (*entity.Job, error)
	MarkDone(ctx context.Context, id uint64, resultMetadata []byte) error
	MarkFailed(ctx context.Context, id uint64, resultMetadata []byte) error
	RequeueStuckJobs(ctx context.Context, stuckBefore time.Time) (int64, error)
}
