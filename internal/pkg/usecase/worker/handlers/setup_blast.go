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

const setupBlastScript = "/app/scripts/setup_blast.sh"

// SetupBlastHandler runs makeblastdb to build a SequenceServer-compatible
// BLAST database from an uploaded file.
type SetupBlastHandler struct {
	dbType          string
	title           string
	out             string
	containerName   string
	jobRepo         IJobRepository
	uploadFileRepo  IUploadFileRepository
	appSettingsRepo IAppSettingsRepository
}

func NewSetupBlastHandler(dbType, title, out, containerName string, jobRepo IJobRepository, uploadFileRepo IUploadFileRepository, appSettingsRepo IAppSettingsRepository) *SetupBlastHandler {
	return &SetupBlastHandler{
		dbType:          dbType,
		title:           title,
		out:             out,
		containerName:   containerName,
		jobRepo:         jobRepo,
		uploadFileRepo:  uploadFileRepo,
		appSettingsRepo: appSettingsRepo,
	}
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

// OnComplete checks whether all three blast databases are ready for this version
// by finding the latest completed file of each type and confirming its blast job
// is DONE. If all three are ready, it sets the default version and restarts the
// blast container.
func (h *SetupBlastHandler) OnComplete(ctx context.Context, job entity.Job) error {
	type blastSpec struct {
		fileType string
		jobType  string
	}
	specs := []blastSpec{
		{entity.FileTypeGenomicFNA, entity.JobTypeGenomicFNASetupBlast},
		{entity.FileTypeProteinFAA, entity.JobTypeProteinFAASetupBlast},
		{entity.FileTypeRNAFNA, entity.JobTypeRNAFNASetupBlast},
	}

	for _, spec := range specs {
		f, err := h.uploadFileRepo.FindLatestCompletedByVersionAndType(ctx, job.VersionID, spec.fileType)
		if err != nil {
			log.Ctx(ctx).Warn().Err(err).Str("fileType", spec.fileType).Msg("failed to find latest file for blast check")
			return nil
		}
		if f == nil {
			return nil // file not uploaded yet
		}
		done, err := h.jobRepo.HasDoneJobOfTypeForFile(ctx, f.ID, spec.jobType)
		if err != nil {
			log.Ctx(ctx).Warn().Err(err).Str("jobType", spec.jobType).Msg("failed to check blast job status")
			return nil
		}
		if !done {
			return nil
		}
	}

	// All three blast databases are built — promote this version as default.
	if err := h.appSettingsRepo.SetDefaultVersion(ctx, job.VersionID); err != nil {
		log.Ctx(ctx).Warn().Err(err).Uint64("versionID", job.VersionID).Msg("failed to set default version after blast setup")
		return nil
	}

	if h.containerName != "" {
		if err := restartDockerContainer(ctx, h.containerName); err != nil {
			log.Ctx(ctx).Warn().Err(err).Str("container", h.containerName).Msg("failed to restart blast container")
			return nil
		}
	}

	log.Ctx(ctx).Info().
		Uint64("versionID", job.VersionID).
		Str("container", h.containerName).
		Msg("all blast databases ready: default version set and blast container restarted")

	return nil
}

func restartDockerContainer(ctx context.Context, containerName string) error {
	cmd := exec.CommandContext(ctx, "docker", "restart", containerName)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker restart %s: %w\n%s", containerName, err, out)
	}
	return nil
}
