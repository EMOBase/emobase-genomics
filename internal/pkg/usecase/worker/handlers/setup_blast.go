package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	"github.com/EMOBase/emobase-genomics/internal/pkg/jobpayload"
	ucworker "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/worker"
	"github.com/rs/zerolog/log"
)

const setupBlastScript = "/app/scripts/setup_blast.sh"

// SetupBlastHandler runs makeblastdb to build a SequenceServer-compatible
// BLAST database from an uploaded file.
type SetupBlastHandler struct {
	dbType          string         // "nucl" or "prot"
	title           string         // full title passed to makeblastdb -title
	out             string         // output database path (e.g. "/db/genome")
	jobRepo         IJobRepository // non-nil only when triggerJBrowse2 is true
	versionRepo     IVersionRepository
	triggerJBrowse2 bool
}

func NewSetupBlastHandler(dbType, title, out string) *SetupBlastHandler {
	return &SetupBlastHandler{dbType: dbType, title: title, out: out}
}

// WithJBrowse2Trigger configures the handler to attempt enqueuing a
// GENOMIC.FNA:SETUP_JBROWSE2 job after a successful run.
func (h *SetupBlastHandler) WithJBrowse2Trigger(jobRepo IJobRepository, versionRepo IVersionRepository) *SetupBlastHandler {
	h.jobRepo = jobRepo
	h.versionRepo = versionRepo
	h.triggerJBrowse2 = true
	return h
}

func (h *SetupBlastHandler) Handle(ctx context.Context, job entity.Job) error {
	var payload jobpayload.SetupBlastPayload
	if err := json.Unmarshal(*job.Payload, &payload); err != nil {
		return fmt.Errorf("failed to unmarshal setup_blast payload: %w", err)
	}

	cmd := exec.CommandContext(ctx, setupBlastScript,
		payload.FilePath, h.dbType, h.title, h.out,
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("setup_blast script failed: %w\noutput: %s", err, out)
	}

	log.Ctx(ctx).Info().
		Str("jobType", job.Type).
		Str("out", h.out).
		Msg("makeblastdb completed successfully")

	return nil
}

func (h *SetupBlastHandler) OnComplete(ctx context.Context, job entity.Job) error {
	if !h.triggerJBrowse2 {
		return nil
	}
	if err := tryEnqueueSetupJBrowse2(ctx, h.jobRepo, h.versionRepo, job.VersionID); err != nil {
		log.Ctx(ctx).Warn().Err(err).Msg("failed to enqueue setup_jbrowse2 after setup_blast")
	}
	return nil
}

// enqueueSetupBlastJob creates a follow-up SETUP_BLAST job after a sequence
// processing job completes. filePath must point to the processed file.
func enqueueSetupBlastJob(ctx context.Context, jobRepo IJobRepository, sourceJob entity.Job, jobType string) error {
	var sourcePayload jobpayload.ProcessPayload
	if err := json.Unmarshal(*sourceJob.Payload, &sourcePayload); err != nil {
		return fmt.Errorf("failed to unmarshal source job payload: %w", err)
	}

	rawPayload, err := json.Marshal(jobpayload.SetupBlastPayload{FilePath: sourcePayload.FilePath})
	if err != nil {
		return fmt.Errorf("failed to marshal setup blast payload: %w", err)
	}

	p := json.RawMessage(rawPayload)
	j := &entity.Job{
		VersionID:   sourceJob.VersionID,
		Type:        jobType,
		Description: ucworker.JobDescriptions[jobType],
		Payload:     &p,
		Status:      entity.JobStatusPending,
	}

	if err := jobRepo.Create(ctx, j); err != nil {
		return fmt.Errorf("failed to create %s job: %w", jobType, err)
	}

	log.Ctx(ctx).Info().
		Str("jobType", jobType).
		Uint64("jobID", j.ID).
		Str("filePath", sourcePayload.FilePath).
		Msg("setup blast job enqueued")

	return nil
}
