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
// BLAST database from an uploaded file. Output paths and titles are computed
// per job from the payload's AssemblyID/AssemblyName — BLAST DB filenames are
// flat, per-(assembly, type) names under one shared blastDBPath, not 3 fixed
// global slots, since every assembly under the default version is
// simultaneously BLASTable.
type SetupBlastHandler struct {
	dbType              string // makeblastdb -dbtype: "nucl" or "prot"
	typeLabel           string // e.g. "Genome", "Protein", "RNA" — for the -title
	dbSuffix            string // e.g. "genome", "protein", "rna" — for the -out filename
	blastDBPath         string
	blastTitle          string
	containerName       string
	jobRepo             IJobRepository
	appSettingsRepo     IAppSettingsRepository
	assemblyVersionRepo IAssemblyVersionRepository
}

func NewSetupBlastHandler(
	dbType, typeLabel, dbSuffix, blastDBPath, blastTitle, containerName string,
	jobRepo IJobRepository,
	appSettingsRepo IAppSettingsRepository,
	assemblyVersionRepo IAssemblyVersionRepository,
) *SetupBlastHandler {
	return &SetupBlastHandler{
		dbType:              dbType,
		typeLabel:           typeLabel,
		dbSuffix:            dbSuffix,
		blastDBPath:         blastDBPath,
		blastTitle:          blastTitle,
		containerName:       containerName,
		jobRepo:             jobRepo,
		appSettingsRepo:     appSettingsRepo,
		assemblyVersionRepo: assemblyVersionRepo,
	}
}

func (h *SetupBlastHandler) Handle(ctx context.Context, job entity.Job) (json.RawMessage, error) {
	var payload jobpayload.SetupBlastPayload
	if err := json.Unmarshal(*job.Payload, &payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal setup_blast payload: %w", err)
	}

	out := fmt.Sprintf("%s/%s-%s", h.blastDBPath, payload.AssemblyID, h.dbSuffix)
	title := fmt.Sprintf("%s %s %s", h.blastTitle, payload.AssemblyName, h.typeLabel)

	cmd := exec.CommandContext(ctx, setupBlastScript,
		payload.FilePath, h.dbType, title, out,
	)

	cmdOut, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("setup_blast script failed: %w\noutput: %s", err, cmdOut)
	}

	log.Ctx(ctx).Info().
		Str("jobType", job.Type).
		Str("out", out).
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
	return finalizeBlastRelease(ctx, h.jobRepo, h.appSettingsRepo, h.assemblyVersionRepo, job.VersionID, payload.VersionName, h.containerName, h.blastDBPath)
}
