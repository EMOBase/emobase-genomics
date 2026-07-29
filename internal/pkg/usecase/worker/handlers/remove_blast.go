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

const removeBlastScript = "/app/scripts/remove_blast.sh"

// RemoveBlastHandler deletes a BLAST database that a released version no
// longer has a source file for, so the blast server never keeps serving data
// from an older, superseded version (see ReleaseVersion).
type RemoveBlastHandler struct {
	out             string
	containerName   string
	jobRepo         IJobRepository
	appSettingsRepo IAppSettingsRepository
}

func NewRemoveBlastHandler(out, containerName string, jobRepo IJobRepository, appSettingsRepo IAppSettingsRepository) *RemoveBlastHandler {
	return &RemoveBlastHandler{
		out:             out,
		containerName:   containerName,
		jobRepo:         jobRepo,
		appSettingsRepo: appSettingsRepo,
	}
}

func (h *RemoveBlastHandler) Handle(ctx context.Context, job entity.Job) (json.RawMessage, error) {
	cmd := exec.CommandContext(ctx, removeBlastScript, h.out)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("remove_blast script failed: %w\noutput: %s", err, out)
	}

	log.Ctx(ctx).Info().
		Str("jobType", job.Type).
		Str("out", h.out).
		Msg("stale blast database removed")

	return nil, nil
}

// OnComplete promotes this version as the default once all enqueued blast jobs
// for it (setup and remove alike) are done.
func (h *RemoveBlastHandler) OnComplete(ctx context.Context, job entity.Job, _ json.RawMessage) error {
	var payload jobpayload.RemoveBlastPayload
	if err := json.Unmarshal(*job.Payload, &payload); err != nil {
		log.Ctx(ctx).Warn().Err(err).Msg("failed to unmarshal remove_blast payload in OnComplete")
		return nil
	}
	return finalizeBlastRelease(ctx, h.jobRepo, h.appSettingsRepo, job.VersionID, payload.VersionName, h.containerName)
}
