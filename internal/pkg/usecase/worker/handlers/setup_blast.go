package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	"github.com/EMOBase/emobase-genomics/internal/pkg/jobpayload"
	"github.com/rs/zerolog/log"
)

// SetupBlastHandler runs makeblastdb to build a SequenceServer-compatible
// BLAST database from an uploaded file.
type SetupBlastHandler struct {
	dbType string // "nucl" or "prot"
	title  string // full title passed to makeblastdb -title
	out    string // output database path (e.g. "/db/genome")
}

func NewSetupBlastHandler(dbType, title, out string) *SetupBlastHandler {
	return &SetupBlastHandler{dbType: dbType, title: title, out: out}
}

func (h *SetupBlastHandler) Handle(ctx context.Context, job entity.Job) error {
	var payload jobpayload.SetupBlastPayload
	if err := json.Unmarshal(*job.Payload, &payload); err != nil {
		return fmt.Errorf("failed to unmarshal setup_blast payload: %w", err)
	}

	cmd := exec.CommandContext(ctx, "makeblastdb",
		"-in", payload.FilePath,
		"-dbtype", h.dbType,
		"-title", h.title,
		"-parse_seqids",
		"-out", h.out,
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("makeblastdb failed: %w\noutput: %s", err, out)
	}

	log.Ctx(ctx).Info().
		Str("jobType", job.Type).
		Str("out", h.out).
		Msg("makeblastdb completed successfully")

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
		VersionID: sourceJob.VersionID,
		Type:      jobType,
		Payload:   &p,
		Status:    entity.JobStatusPending,
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
