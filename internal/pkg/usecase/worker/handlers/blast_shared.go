package handlers

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"

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
// container, cleans up the previous default version's now-superseded BLAST
// files, and promotes every one of this version's assemblies' JBrowse2 views
// to the front (all equally, since assemblies are fully symmetric — there is
// no single "the" assembly anymore) once all blast setup/remove jobs for it
// are done. ReleaseVersion already validates prerequisites before enqueuing
// these jobs, so no further file-type checks are needed here.
func finalizeBlastRelease(ctx context.Context, jobRepo IJobRepository, appSettingsRepo IAppSettingsRepository, assemblyVersionRepo IAssemblyVersionRepository, versionID uint64, versionName string, containerName, blastDBPath string) error {
	hasPending, err := jobRepo.HasNonDoneJobOfTypesForVersion(ctx, versionID, blastJobTypes)
	if err != nil {
		log.Ctx(ctx).Warn().Err(err).Msg("failed to check blast job statuses")
		return nil
	}
	if hasPending {
		return nil
	}

	prevDefaultID, err := appSettingsRepo.GetDefaultVersionID(ctx)
	if err != nil {
		log.Ctx(ctx).Warn().Err(err).Msg("failed to read previous default version before promotion")
		return nil
	}

	if err := appSettingsRepo.SetDefaultVersion(ctx, versionID); err != nil {
		log.Ctx(ctx).Warn().Err(err).Uint64("versionID", versionID).Msg("failed to set default version after blast setup")
		return nil
	}

	// BLAST DB filenames are keyed by assembly, not by "the" 3 fixed global
	// slots today's single-species deployments used — so switching the
	// default version no longer discards the previous default's files for
	// free. Remove them explicitly, or SequenceServer would keep showing a
	// stale, no-longer-default version's species alongside the current one.
	if prevDefaultID != nil && *prevDefaultID != versionID {
		removeStaleBlastFiles(ctx, blastDBPath, *prevDefaultID)
	}

	if containerName != "" {
		if err := restartDockerContainer(ctx, containerName); err != nil {
			log.Ctx(ctx).Warn().Err(err).Str("container", containerName).Msg("failed to restart blast container")
			return nil
		}
	}

	assemblies, err := assemblyVersionRepo.ListByVersionID(ctx, versionID)
	if err != nil {
		log.Ctx(ctx).Warn().Err(err).Uint64("versionID", versionID).Msg("failed to list assemblies for JBrowse2 default view promotion")
		return nil
	}
	if err := promoteAllAssemblyViews(ctx, assemblies); err != nil {
		log.Ctx(ctx).Warn().Err(err).Str("version", versionName).Msg("failed to promote JBrowse2 default views")
		return nil
	}

	log.Ctx(ctx).Info().
		Uint64("versionID", versionID).
		Str("version", versionName).
		Str("container", containerName).
		Msg("all blast databases ready: default version set, blast container restarted, and JBrowse2 default views promoted")

	return nil
}

// promoteAllAssemblyViews moves every one of this version's assemblies' views
// to the front of JBrowse2's defaultSession.views, in descending AssemblyID
// order — set_default_jbrowse2_view.sh moves one view to index 0 per
// invocation, so calling it once per assembly from highest to lowest ID
// leaves the lowest-ID assembly at the very front, giving a final ascending-
// by-AssemblyID order with no script changes needed. No-op for an assembly
// with no JBrowse2 view yet (e.g. released without a genomic.gff upload).
func promoteAllAssemblyViews(ctx context.Context, assemblies []entity.AssemblyVersion) error {
	sort.Slice(assemblies, func(i, j int) bool { return assemblies[i].ID > assemblies[j].ID })
	for _, asm := range assemblies {
		if err := setDefaultJBrowse2View(ctx, asm.AssemblyID()); err != nil {
			return err
		}
	}
	return nil
}

// setDefaultJBrowse2View moves one assembly's view to the front of JBrowse2's
// defaultSession.views (so it opens first with no explicit session) and
// prunes any views whose assembly no longer exists.
func setDefaultJBrowse2View(ctx context.Context, assemblyID string) error {
	out, err := exec.CommandContext(ctx, setDefaultJBrowse2ViewScript, assemblyID).CombinedOutput()
	if err != nil {
		return fmt.Errorf("set_default_jbrowse2_view script failed: %w\noutput: %s", err, out)
	}
	return nil
}

// removeStaleBlastFiles deletes every BLAST database file belonging to any
// assembly under the given (now-superseded) Database Version ID. AssemblyID
// values are shaped "v{versionID}a{assemblyID}", so a plain glob on the
// version-ID prefix (followed by the literal "a" delimiter, which prevents a
// version ID from matching as a prefix of a longer one, e.g. "v1a*" vs.
// "v12a3...") finds every type/extension for every one of that version's
// assemblies without needing to query which assemblies it had.
func removeStaleBlastFiles(ctx context.Context, blastDBPath string, oldVersionID uint64) {
	pattern := filepath.Join(blastDBPath, fmt.Sprintf("v%da*", oldVersionID))
	matches, err := filepath.Glob(pattern)
	if err != nil {
		log.Ctx(ctx).Warn().Err(err).Str("pattern", pattern).Msg("failed to glob stale blast files")
		return
	}
	for _, m := range matches {
		if err := os.Remove(m); err != nil && !os.IsNotExist(err) {
			log.Ctx(ctx).Warn().Err(err).Str("path", m).Msg("failed to remove stale blast file")
		}
	}
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
