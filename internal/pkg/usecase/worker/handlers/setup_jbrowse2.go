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

const setupJBrowse2Script = "/app/scripts/setup_jbrowse2.sh"

// SetupJBrowse2Handler runs the JBrowse2 setup script to build genome browser
// tracks from the genomic FNA and GFF files.
type SetupJBrowse2Handler struct{}

func NewSetupJBrowse2Handler() *SetupJBrowse2Handler {
	return &SetupJBrowse2Handler{}
}

func (h *SetupJBrowse2Handler) Handle(ctx context.Context, job entity.Job) error {
	var payload jobpayload.SetupJBrowse2Payload
	if err := json.Unmarshal(*job.Payload, &payload); err != nil {
		return fmt.Errorf("failed to unmarshal setup_jbrowse2 payload: %w", err)
	}

	cmd := exec.CommandContext(ctx, setupJBrowse2Script, payload.GenomicFNAPath, payload.GenomicGFFPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("setup_jbrowse2 script failed: %w\noutput: %s", err, out)
	}

	log.Ctx(ctx).Info().
		Uint64("jobID", job.ID).
		Msg("JBrowse2 setup completed successfully")

	return nil
}

// tryEnqueueSetupJBrowse2 checks whether all prerequisite jobs (GENOMIC.GFF and
// GENOMIC.FNA:SETUP_BLAST) are done for the version, and if so enqueues a
// GENOMIC.FNA:SETUP_JBROWSE2 job. Safe to call from either prerequisite handler
// — it is a no-op when prerequisites are incomplete or the job already exists.
func tryEnqueueSetupJBrowse2(ctx context.Context, jobRepo IJobRepository, versionID uint64) error {
	prereqs := []string{ucworker.JobTypeGenomicGFF, ucworker.JobTypeGenomicFNASetupBlast}

	doneJobs, err := jobRepo.FindDoneByVersionAndTypes(ctx, versionID, prereqs)
	if err != nil {
		return fmt.Errorf("failed to check prerequisite jobs: %w", err)
	}
	if len(doneJobs) < len(prereqs) {
		return nil // not all prerequisites done yet
	}

	// Guard against duplicate enqueuing (race condition or re-upload).
	exists, err := jobRepo.HasNonFailedJobOfType(ctx, versionID, ucworker.JobTypeGenomicFNASetupJBrowse2)
	if err != nil {
		return fmt.Errorf("failed to check existing jbrowse2 job: %w", err)
	}
	if exists {
		return nil
	}

	// Extract file paths from the completed prerequisite jobs.
	var fnaPath, gffPath string
	for _, j := range doneJobs {
		switch j.Type {
		case ucworker.JobTypeGenomicFNASetupBlast:
			var p jobpayload.SetupBlastPayload
			if err := json.Unmarshal(*j.Payload, &p); err != nil {
				return fmt.Errorf("failed to unmarshal setup_blast payload: %w", err)
			}
			fnaPath = p.FilePath
		case ucworker.JobTypeGenomicGFF:
			var p jobpayload.ProcessPayload
			if err := json.Unmarshal(*j.Payload, &p); err != nil {
				return fmt.Errorf("failed to unmarshal genomic_gff payload: %w", err)
			}
			gffPath = p.FilePath
		}
	}

	rawPayload, err := json.Marshal(jobpayload.SetupJBrowse2Payload{
		GenomicFNAPath: fnaPath,
		GenomicGFFPath: gffPath,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal setup_jbrowse2 payload: %w", err)
	}

	p := json.RawMessage(rawPayload)
	j := &entity.Job{
		VersionID: versionID,
		Type:      ucworker.JobTypeGenomicFNASetupJBrowse2,
		Payload:   &p,
		Status:    entity.JobStatusPending,
	}

	if err := jobRepo.Create(ctx, j); err != nil {
		return fmt.Errorf("failed to create setup_jbrowse2 job: %w", err)
	}

	log.Ctx(ctx).Info().
		Uint64("jobID", j.ID).
		Str("fnaPath", fnaPath).
		Str("gffPath", gffPath).
		Msg("setup_jbrowse2 job enqueued")

	return nil
}
