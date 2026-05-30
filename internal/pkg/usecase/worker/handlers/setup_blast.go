package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
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
	blastJobTypes := []string{
		entity.JobTypeGenomicFNASetupBlast,
		entity.JobTypeProteinFAASetupBlast,
		entity.JobTypeRNAFNASetupBlast,
	}

	hasPending, err := h.jobRepo.HasNonDoneJobOfTypesForVersion(ctx, job.VersionID, blastJobTypes)
	if err != nil {
		log.Ctx(ctx).Warn().Err(err).Msg("failed to check blast job statuses")
		return nil
	}
	if hasPending {
		return nil
	}

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
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", "/var/run/docker.sock")
		},
	}
	client := &http.Client{Transport: transport}

	url := fmt.Sprintf("http://localhost/containers/%s/restart", containerName)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return fmt.Errorf("docker restart %s: %w", containerName, err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("docker restart %s: %w", containerName, err)
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); resp.Body.Close() }()

	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("docker restart %s: unexpected status %d: %s", containerName, resp.StatusCode, body)
	}
	return nil
}
