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
	appSettingsRepo IAppSettingsRepository
}

func NewSetupBlastHandler(dbType, title, out, containerName string, jobRepo IJobRepository, appSettingsRepo IAppSettingsRepository) *SetupBlastHandler {
	return &SetupBlastHandler{
		dbType:          dbType,
		title:           title,
		out:             out,
		containerName:   containerName,
		jobRepo:         jobRepo,
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

// OnComplete checks whether all three blast job types are done for this version.
// If so, it sets the default version and restarts the blast container.
func (h *SetupBlastHandler) OnComplete(ctx context.Context, job entity.Job) error {
	blastTypes := []string{
		entity.JobTypeGenomicFNASetupBlast,
		entity.JobTypeProteinFAASetupBlast,
		entity.JobTypeRNAFNASetupBlast,
	}

	for _, t := range blastTypes {
		done, err := h.jobRepo.IsLatestJobDoneByType(ctx, job.VersionID, t)
		if err != nil {
			log.Ctx(ctx).Warn().Err(err).Str("jobType", t).Msg("failed to check blast job status")
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
