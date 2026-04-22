package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	"github.com/EMOBase/emobase-genomics/internal/pkg/jobpayload"
	"github.com/rs/zerolog/log"
)

type IDeleteUploadFileRepository interface {
	FindByID(ctx context.Context, id string) (*entity.UploadFile, error)
	SoftDelete(ctx context.Context, id string, deletedBy string) error
}

type IDeleteOrthologyRepository interface {
	DeleteByFileID(ctx context.Context, indexName, fileID string) error
}

type DeleteOrthologyTSVHandler struct {
	uploadDir      string
	uploadFileRepo IDeleteUploadFileRepository
	versionRepo    IVersionRepository
	orthologyRepo  IDeleteOrthologyRepository
}

func NewDeleteOrthologyTSVHandler(
	uploadDir string,
	uploadFileRepo IDeleteUploadFileRepository,
	versionRepo IVersionRepository,
	orthologyRepo IDeleteOrthologyRepository,
) *DeleteOrthologyTSVHandler {
	return &DeleteOrthologyTSVHandler{
		uploadDir:      uploadDir,
		uploadFileRepo: uploadFileRepo,
		versionRepo:    versionRepo,
		orthologyRepo:  orthologyRepo,
	}
}

func (h *DeleteOrthologyTSVHandler) Handle(ctx context.Context, job entity.Job) error {
	var payload jobpayload.DeleteFilePayload
	if err := json.Unmarshal(*job.Payload, &payload); err != nil {
		return fmt.Errorf("failed to unmarshal job payload: %w", err)
	}

	f, err := h.uploadFileRepo.FindByID(ctx, payload.UploadFileID)
	if err != nil {
		return fmt.Errorf("failed to look up upload file: %w", err)
	}
	if f == nil {
		return fmt.Errorf("upload file %q not found", payload.UploadFileID)
	}

	version, err := h.versionRepo.FindByID(ctx, f.VersionID)
	if err != nil {
		return fmt.Errorf("failed to look up version: %w", err)
	}
	if version == nil {
		return fmt.Errorf("version %d not found", f.VersionID)
	}

	indexName := fmt.Sprintf("emobasegenomics-orthology-%s-%d",
		strings.ToLower(version.Name), version.CreatedAt.Unix())

	if err := h.orthologyRepo.DeleteByFileID(ctx, indexName, payload.UploadFileID); err != nil {
		return fmt.Errorf("failed to delete orthology records: %w", err)
	}

	if err := h.uploadFileRepo.SoftDelete(ctx, payload.UploadFileID, payload.DeletedBy); err != nil {
		return fmt.Errorf("failed to soft-delete upload file record: %w", err)
	}

	filePath := filepath.Join(h.uploadDir, f.FilePath)
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		log.Ctx(ctx).Warn().Err(err).Str("path", filePath).Msg("failed to remove orthology file from disk")
	}

	return nil
}
