package worker

import (
	"context"
	"encoding/json"
	"time"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	"github.com/rs/zerolog/log"
)

type Worker struct {
	jobRepo       IJobRepository
	handlers      map[string]Handler
	pollInterval  time.Duration
	stuckInterval time.Duration
	stuckTimeout  time.Duration
}

func New(
	jobRepo IJobRepository,
	handlers map[string]Handler,
	pollInterval time.Duration,
	stuckInterval time.Duration,
	stuckTimeout time.Duration,
) *Worker {
	return &Worker{
		jobRepo:       jobRepo,
		handlers:      handlers,
		pollInterval:  pollInterval,
		stuckInterval: stuckInterval,
		stuckTimeout:  stuckTimeout,
	}
}

func (w *Worker) Run(ctx context.Context) error {
	go w.runStuckJobRecovery(ctx)

	for {
		job, err := w.jobRepo.ClaimNextPending(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			log.Error().Err(err).Msg("failed to claim pending job")
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(w.pollInterval):
			}
			continue
		}

		if job == nil {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(w.pollInterval):
			}
			continue
		}

		w.processJob(ctx, job)
	}
}

// OnCompleteHook is an optional interface handlers can implement to run logic
// after the job has been marked DONE in the database.
type OnCompleteHook interface {
	OnComplete(ctx context.Context, job entity.Job) error
}

// OnFailureHook is an optional interface handlers can implement to run logic
// after the job has been marked FAILED in the database.
type OnFailureHook interface {
	OnFailure(ctx context.Context, job entity.Job, err error) error
}

func (w *Worker) processJob(ctx context.Context, job *entity.Job) {
	logger := log.With().Uint64("jobID", job.ID).Str("jobType", job.Type).Logger()

	handler, ok := w.handlers[job.Type]
	if !ok {
		logger.Error().Msg("no handler registered for job type")
		meta, _ := json.Marshal(map[string]string{"error": "no handler for job type: " + job.Type})
		_ = w.jobRepo.MarkFailed(ctx, job.ID, meta)
		return
	}

	logger.Info().Msg("processing job")

	if err := handler.Handle(ctx, *job); err != nil {
		logger.Error().Err(err).Msg("job handler failed")
		meta, _ := json.Marshal(map[string]string{"error": err.Error()})
		_ = w.jobRepo.MarkFailed(ctx, job.ID, meta)

		if hook, ok := handler.(OnFailureHook); ok {
			if hookErr := hook.OnFailure(ctx, *job, err); hookErr != nil {
				logger.Warn().Err(hookErr).Msg("post-failure hook failed")
			}
		}
		return
	}

	if err := w.jobRepo.MarkDone(ctx, job.ID, nil); err != nil {
		logger.Error().Err(err).Msg("failed to mark job done")
		return
	}

	logger.Info().Msg("job completed")

	if hook, ok := handler.(OnCompleteHook); ok {
		if err := hook.OnComplete(ctx, *job); err != nil {
			logger.Warn().Err(err).Msg("post-completion hook failed")
		}
	}
}

func (w *Worker) runStuckJobRecovery(ctx context.Context) {
	ticker := time.NewTicker(w.stuckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			stuckBefore := time.Now().UTC().Add(-w.stuckTimeout)
			count, err := w.jobRepo.RequeueStuckJobs(ctx, stuckBefore)
			if err != nil {
				log.Error().Err(err).Msg("failed to requeue stuck jobs")
				continue
			}
			if count > 0 {
				log.Info().Int64("count", count).Msg("requeued stuck jobs back to PENDING")
			}
		}
	}
}
