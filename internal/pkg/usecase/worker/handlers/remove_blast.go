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

// RemoveBlastHandler deletes a BLAST database that a released assembly no
// longer has a source file for, so the blast server never keeps serving data
// for a file type an assembly no longer has (see ReleaseVersion).
type RemoveBlastHandler struct {
	dbSuffix            string // e.g. "protein", "rna" — for the -out filename
	blastDBPath         string
	containerName       string
	jobRepo             IJobRepository
	appSettingsRepo     IAppSettingsRepository
	assemblyVersionRepo IAssemblyVersionRepository
}

func NewRemoveBlastHandler(
	dbSuffix, blastDBPath, containerName string,
	jobRepo IJobRepository,
	appSettingsRepo IAppSettingsRepository,
	assemblyVersionRepo IAssemblyVersionRepository,
) *RemoveBlastHandler {
	return &RemoveBlastHandler{
		dbSuffix:            dbSuffix,
		blastDBPath:         blastDBPath,
		containerName:       containerName,
		jobRepo:             jobRepo,
		appSettingsRepo:     appSettingsRepo,
		assemblyVersionRepo: assemblyVersionRepo,
	}
}

func (h *RemoveBlastHandler) Handle(ctx context.Context, job entity.Job) (json.RawMessage, error) {
	var payload jobpayload.RemoveBlastPayload
	if err := json.Unmarshal(*job.Payload, &payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal remove_blast payload: %w", err)
	}

	out := fmt.Sprintf("%s/%s-%s", h.blastDBPath, payload.AssemblyID, h.dbSuffix)
	cmd := exec.CommandContext(ctx, removeBlastScript, out)

	cmdOut, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("remove_blast script failed: %w\noutput: %s", err, cmdOut)
	}

	log.Ctx(ctx).Info().
		Str("jobType", job.Type).
		Str("out", out).
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
	return finalizeBlastRelease(ctx, h.jobRepo, h.appSettingsRepo, h.assemblyVersionRepo, job.VersionID, payload.VersionName, h.containerName, h.blastDBPath)
}
