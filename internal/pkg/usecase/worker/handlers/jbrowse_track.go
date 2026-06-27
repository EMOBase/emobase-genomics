package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	"github.com/EMOBase/emobase-genomics/internal/pkg/jobpayload"
	"github.com/rs/zerolog/log"
)

const (
	jbrowseBin            = "jbrowse"
	jbrowseOutDir         = "/web/data"
	addJBrowseTrackScript = "/app/scripts/add_jbrowse_track.sh"
)

// JBrowseTrackHandler runs `jbrowse add-track` to register an uploaded track file.
type JBrowseTrackHandler struct{}

func NewJBrowseTrackHandler() *JBrowseTrackHandler {
	return &JBrowseTrackHandler{}
}

func (h *JBrowseTrackHandler) Handle(ctx context.Context, job entity.Job) (json.RawMessage, error) {
	var payload jobpayload.JBrowseTrackPayload
	if err := json.Unmarshal(*job.Payload, &payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal %s payload: %w", entity.JobTypeJBrowseTrack, err)
	}

	trackID := "track-" + payload.FileID
	trackName := payload.VersionName + " " + payload.TrackName

	cmd := exec.CommandContext(ctx, addJBrowseTrackScript,
		payload.FilePath,
		trackName,
		payload.VersionName,
		trackID,
		payload.Category,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s script failed: %w\noutput: %s", entity.JobTypeJBrowseTrack, err, out)
	}

	log.Ctx(ctx).Info().
		Uint64("jobID", job.ID).
		Str("trackId", trackID).
		Str("version", payload.VersionName).
		Str("scriptOutput", string(out)).
		Msgf("%s completed successfully", entity.JobTypeJBrowseTrack)

	return nil, nil
}

// DeleteJBrowseTrackHandler runs `jbrowse remove-track`, soft-deletes the DB record,
// and removes the physical file from disk.
type DeleteJBrowseTrackHandler struct {
	uploadFileRepo IDeleteUploadFileRepository
}

func NewDeleteJBrowseTrackHandler(uploadFileRepo IDeleteUploadFileRepository) *DeleteJBrowseTrackHandler {
	return &DeleteJBrowseTrackHandler{uploadFileRepo: uploadFileRepo}
}

func (h *DeleteJBrowseTrackHandler) Handle(ctx context.Context, job entity.Job) (json.RawMessage, error) {
	var payload jobpayload.DeleteFilePayload
	if err := json.Unmarshal(*job.Payload, &payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal %s payload: %w", entity.JobTypeJBrowseTrackDelete, err)
	}

	trackID := "track-" + payload.UploadFileID
	cmd := exec.CommandContext(ctx, jbrowseBin, "remove-track", trackID, "--out", jbrowseOutDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s failed: %w\noutput: %s", entity.JobTypeJBrowseTrackDelete, err, out)
	}

	f, err := h.uploadFileRepo.FindByID(ctx, payload.UploadFileID)
	if err != nil {
		return nil, fmt.Errorf("failed to look up upload file: %w", err)
	}
	if f == nil {
		return nil, fmt.Errorf("upload file %q not found", payload.UploadFileID)
	}

	if err := h.uploadFileRepo.SoftDelete(ctx, payload.UploadFileID, payload.DeletedBy); err != nil {
		return nil, fmt.Errorf("failed to soft-delete upload file record: %w", err)
	}

	if err := os.Remove(f.FilePath); err != nil && !os.IsNotExist(err) {
		log.Ctx(ctx).Warn().Err(err).Str("path", f.FilePath).Msg("failed to remove jbrowse track file from disk")
	}

	log.Ctx(ctx).Info().
		Uint64("jobID", job.ID).
		Str("trackId", trackID).
		Msgf("%s completed successfully", entity.JobTypeJBrowseTrackDelete)

	return nil, nil
}
