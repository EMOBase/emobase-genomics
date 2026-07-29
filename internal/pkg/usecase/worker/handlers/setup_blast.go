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

func (h *SetupBlastHandler) Handle(ctx context.Context, job entity.Job) (json.RawMessage, error) {
	var payload jobpayload.SetupBlastPayload
	if err := json.Unmarshal(*job.Payload, &payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal setup_blast payload: %w", err)
	}

	cmd := exec.CommandContext(ctx, setupBlastScript,
		payload.FilePath, h.dbType, h.title, h.out,
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("setup_blast script failed: %w\noutput: %s", err, out)
	}

	log.Ctx(ctx).Info().
		Str("jobType", job.Type).
		Str("out", h.out).
		Msg("makeblastdb completed successfully")

	return nil, nil
}

// OnComplete promotes this version as the default once all enqueued blast jobs
// for it are done. Since ReleaseVersion already validates prerequisites before
// enqueuing blast jobs, no further file-type checks are needed here.
func (h *SetupBlastHandler) OnComplete(ctx context.Context, job entity.Job, _ json.RawMessage) error {
	var payload jobpayload.SetupBlastPayload
	if err := json.Unmarshal(*job.Payload, &payload); err != nil {
		log.Ctx(ctx).Warn().Err(err).Msg("failed to unmarshal setup_blast payload in OnComplete")
		return nil
	}
	return finalizeBlastRelease(ctx, h.jobRepo, h.appSettingsRepo, job.VersionID, payload.VersionName, h.containerName)
}
