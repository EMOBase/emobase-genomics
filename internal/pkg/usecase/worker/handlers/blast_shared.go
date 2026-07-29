package handlers

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	"github.com/rs/zerolog/log"
)

const setDefaultJBrowse2ViewScript = "/app/scripts/set_default_jbrowse2_view.sh"

// blastJobTypes lists every job type that can affect the shared BLAST
// databases for a version — both building (*_SETUP_BLAST) and removing
// (*_REMOVE_BLAST) them. A version is only promoted to default (and the
// blast container restarted) once every job of these types for it is DONE.
var blastJobTypes = []string{
	entity.JobTypeGenomicFNASetupBlast,
	entity.JobTypeProteinFAASetupBlast,
	entity.JobTypeRNAFNASetupBlast,
	entity.JobTypeProteinFAARemoveBlast,
	entity.JobTypeRNAFNARemoveBlast,
}

// finalizeBlastRelease promotes the version to default, restarts the blast
// container, and promotes the version's JBrowse2 assembly to the default view
// once all blast setup/remove jobs for it are done. ReleaseVersion already
// validates prerequisites before enqueuing these jobs, so no further
// file-type checks are needed here.
func finalizeBlastRelease(ctx context.Context, jobRepo IJobRepository, appSettingsRepo IAppSettingsRepository, versionID uint64, versionName string, containerName string) error {
	hasPending, err := jobRepo.HasNonDoneJobOfTypesForVersion(ctx, versionID, blastJobTypes)
	if err != nil {
		log.Ctx(ctx).Warn().Err(err).Msg("failed to check blast job statuses")
		return nil
	}
	if hasPending {
		return nil
	}

	if err := appSettingsRepo.SetDefaultVersion(ctx, versionID); err != nil {
		log.Ctx(ctx).Warn().Err(err).Uint64("versionID", versionID).Msg("failed to set default version after blast setup")
		return nil
	}

	if containerName != "" {
		if err := restartDockerContainer(ctx, containerName); err != nil {
			log.Ctx(ctx).Warn().Err(err).Str("container", containerName).Msg("failed to restart blast container")
			return nil
		}
	}

	if err := setDefaultJBrowse2View(ctx, versionName); err != nil {
		log.Ctx(ctx).Warn().Err(err).Str("version", versionName).Msg("failed to promote JBrowse2 default view")
		return nil
	}

	log.Ctx(ctx).Info().
		Uint64("versionID", versionID).
		Str("version", versionName).
		Str("container", containerName).
		Msg("all blast databases ready: default version set, blast container restarted, and JBrowse2 default view promoted")

	return nil
}

// setDefaultJBrowse2View moves this version's assembly view to the front of
// JBrowse2's defaultSession.views (so it opens first with no explicit
// session) and prunes any views whose assembly no longer exists. No-op if
// this version has no JBrowse2 assembly view (e.g. released without a
// genomic.gff upload).
func setDefaultJBrowse2View(ctx context.Context, versionName string) error {
	out, err := exec.CommandContext(ctx, setDefaultJBrowse2ViewScript, versionName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("set_default_jbrowse2_view script failed: %w\noutput: %s", err, out)
	}
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
