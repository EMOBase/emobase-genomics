package worker

import (
	"context"
	"encoding/json"
	"sync"
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
	maxConcurrent int
}

func New(
	jobRepo IJobRepository,
	handlers map[string]Handler,
	pollInterval time.Duration,
	stuckInterval time.Duration,
	stuckTimeout time.Duration,
	maxConcurrent int,
) *Worker {
	if maxConcurrent <= 0 {
		maxConcurrent = 1
	}
	return &Worker{
		jobRepo:       jobRepo,
		handlers:      handlers,
		pollInterval:  pollInterval,
		stuckInterval: stuckInterval,
		stuckTimeout:  stuckTimeout,
		maxConcurrent: maxConcurrent,
	}
}

func (w *Worker) Run(ctx context.Context) error {
	go w.runStuckJobRecovery(ctx)

	sem := make(chan struct{}, w.maxConcurrent)
	var wg sync.WaitGroup

	for {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return nil
		}

		job, err := w.jobRepo.ClaimNextPending(ctx)
		if err != nil {
			<-sem
			if ctx.Err() != nil {
				wg.Wait()
				return nil
			}
			log.Error().Err(err).Msg("failed to claim pending job")
			select {
			case <-ctx.Done():
				wg.Wait()
				return nil
			case <-time.After(w.pollInterval):
			}
			continue
		}

		if job == nil {
			<-sem
			select {
			case <-ctx.Done():
				wg.Wait()
				return nil
			case <-time.After(w.pollInterval):
			}
			continue
		}

		wg.Add(1)
		go func(job *entity.Job) {
			defer wg.Done()
			defer func() { <-sem }()
			w.processJob(ctx, job)
		}(job)
	}
}

// OnCompleteHook is an optional interface handlers can implement to run logic
// after the job has been marked DONE in the database.
type OnCompleteHook interface {
	OnComplete(ctx context.Context, job entity.Job, result json.RawMessage) error
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

	result, err := handler.Handle(ctx, *job)
	if err != nil {
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

	if err := w.jobRepo.MarkDone(ctx, job.ID, result); err != nil {
		logger.Error().Err(err).Msg("failed to mark job done")
		return
	}

	logger.Info().Msg("job completed")

	if hook, ok := handler.(OnCompleteHook); ok {
		if err := hook.OnComplete(ctx, *job, result); err != nil {
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
