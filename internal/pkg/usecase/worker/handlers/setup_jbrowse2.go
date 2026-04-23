package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	"github.com/EMOBase/emobase-genomics/internal/pkg/jobpayload"
	"github.com/rs/zerolog/log"
)

const (
	setupJBrowse2FNAScript = "/app/scripts/setup_jbrowse2_fna.sh"
	setupJBrowse2GFFScript = "/app/scripts/setup_jbrowse2_gff.sh"
)

// SetupFNAJBrowse2Handler runs the FNA JBrowse2 setup script to register the
// genome assembly. On success it tries to enqueue the GFF setup job.
type SetupFNAJBrowse2Handler struct {
	jobRepo IJobRepository
}

func NewSetupFNAJBrowse2Handler(jobRepo IJobRepository) *SetupFNAJBrowse2Handler {
	return &SetupFNAJBrowse2Handler{jobRepo: jobRepo}
}

func (h *SetupFNAJBrowse2Handler) Handle(ctx context.Context, job entity.Job) error {
	var payload jobpayload.SetupJBrowse2FNAPayload
	if err := json.Unmarshal(*job.Payload, &payload); err != nil {
		return fmt.Errorf("failed to unmarshal %s payload: %w", entity.JobTypeGenomicFNASetupJBrowse2, err)
	}

	cmd := exec.CommandContext(ctx, setupJBrowse2FNAScript, payload.GenomicFNAPath, payload.VersionName)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s script failed: %w\noutput: %s", entity.JobTypeGenomicFNASetupJBrowse2, err, out)
	}

	log.Ctx(ctx).Info().
		Uint64("jobID", job.ID).
		Str("version", payload.VersionName).
		Msgf("%s completed successfully", entity.JobTypeGenomicFNASetupJBrowse2)

	return nil
}

// OnComplete checks whether a done GENOMIC.GFF job exists and, if so, enqueues
// a GENOMIC.GFF:SETUP_JBROWSE2 job for that file.
func (h *SetupFNAJBrowse2Handler) OnComplete(ctx context.Context, job entity.Job) error {
	var payload jobpayload.SetupJBrowse2FNAPayload
	if err := json.Unmarshal(*job.Payload, &payload); err != nil {
		log.Ctx(ctx).Warn().Err(err).Msgf("failed to unmarshal %s payload in OnComplete", entity.JobTypeGenomicFNASetupJBrowse2)
		return nil
	}

	if err := tryEnqueueGFFSetupJBrowse2(ctx, h.jobRepo, job.VersionID, payload.VersionName); err != nil {
		log.Ctx(ctx).Warn().Err(err).Msgf("failed to enqueue %s after %s", entity.JobTypeGenomicGFFSetupJBrowse2, entity.JobTypeGenomicFNASetupJBrowse2)
	}
	return nil
}

// SetupGFFJBrowse2Handler runs the GFF JBrowse2 setup script to add the
// annotation track and rebuild the text index.
type SetupGFFJBrowse2Handler struct{}

func NewSetupGFFJBrowse2Handler() *SetupGFFJBrowse2Handler {
	return &SetupGFFJBrowse2Handler{}
}

func (h *SetupGFFJBrowse2Handler) Handle(ctx context.Context, job entity.Job) error {
	var payload jobpayload.SetupJBrowse2GFFPayload
	if err := json.Unmarshal(*job.Payload, &payload); err != nil {
		return fmt.Errorf("failed to unmarshal %s payload: %w", entity.JobTypeGenomicGFFSetupJBrowse2, err)
	}

	cmd := exec.CommandContext(ctx, setupJBrowse2GFFScript, payload.GenomicGFFPath, payload.VersionName)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s script failed: %w\noutput: %s", entity.JobTypeGenomicGFFSetupJBrowse2, err, out)
	}

	log.Ctx(ctx).Info().
		Uint64("jobID", job.ID).
		Str("version", payload.VersionName).
		Msgf("%s completed successfully", entity.JobTypeGenomicGFFSetupJBrowse2)

	return nil
}

// tryEnqueueGFFSetupJBrowse2 looks up the latest done GENOMIC.GFF job for the
// version and, if found and not already queued, enqueues GENOMIC.GFF:SETUP_JBROWSE2.
func tryEnqueueGFFSetupJBrowse2(ctx context.Context, jobRepo IJobRepository, versionID uint64, versionName string) error {
	doneGFFJobs, err := jobRepo.FindDoneByVersionAndTypes(ctx, versionID, []string{entity.JobTypeGenomicGFF})
	if err != nil {
		return fmt.Errorf("failed to find done %s jobs: %w", entity.JobTypeGenomicGFF, err)
	}
	if len(doneGFFJobs) == 0 {
		return nil // no GFF uploaded yet
	}

	// Pick the latest done GFF job (highest ID = most recent upload).
	latest := doneGFFJobs[0]
	for _, j := range doneGFFJobs[1:] {
		if j.ID > latest.ID {
			latest = j
		}
	}

	if latest.FileID == nil {
		return nil
	}

	exists, err := jobRepo.HasNonFailedJobOfTypeForFile(ctx, *latest.FileID, entity.JobTypeGenomicGFFSetupJBrowse2)
	if err != nil {
		return fmt.Errorf("failed to check existing %s job: %w", entity.JobTypeGenomicGFFSetupJBrowse2, err)
	}
	if exists {
		return nil
	}

	var p jobpayload.ProcessPayload
	if err := json.Unmarshal(*latest.Payload, &p); err != nil {
		return fmt.Errorf("failed to unmarshal %s job payload: %w", entity.JobTypeGenomicGFF, err)
	}

	rawPayload, err := json.Marshal(jobpayload.SetupJBrowse2GFFPayload{
		VersionName:    versionName,
		GenomicGFFPath: p.FilePath,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal %s payload: %w", entity.JobTypeGenomicGFFSetupJBrowse2, err)
	}

	rp := json.RawMessage(rawPayload)
	now := time.Now().UTC()
	j := &entity.Job{
		VersionID:   versionID,
		FileID:      latest.FileID,
		Type:        entity.JobTypeGenomicGFFSetupJBrowse2,
		Description: entity.JobDescriptions[entity.JobTypeGenomicGFFSetupJBrowse2],
		Payload:     &rp,
		Status:      entity.JobStatusPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := jobRepo.Create(ctx, j); err != nil {
		return fmt.Errorf("failed to create %s job: %w", entity.JobTypeGenomicGFFSetupJBrowse2, err)
	}

	log.Ctx(ctx).Info().
		Uint64("jobID", j.ID).
		Str("gffFileID", *latest.FileID).
		Msgf("%s job enqueued", entity.JobTypeGenomicGFFSetupJBrowse2)

	return nil
}
