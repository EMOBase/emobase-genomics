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

type IDeleteSynonymRepository interface {
	DeleteByFileID(ctx context.Context, indexName, fileID string) error
}

type DeleteSynonymHandler struct {
	uploadDir      string
	uploadFileRepo IDeleteUploadFileRepository
	versionRepo    IVersionRepository
	synonymRepo    IDeleteSynonymRepository
	indexPrefix    string
}

func NewDeleteSynonymHandler(
	uploadDir string,
	uploadFileRepo IDeleteUploadFileRepository,
	versionRepo IVersionRepository,
	synonymRepo IDeleteSynonymRepository,
	indexPrefix string,
) *DeleteSynonymHandler {
	return &DeleteSynonymHandler{
		uploadDir:      uploadDir,
		uploadFileRepo: uploadFileRepo,
		versionRepo:    versionRepo,
		synonymRepo:    synonymRepo,
		indexPrefix:    indexPrefix,
	}
}

func (h *DeleteSynonymHandler) Handle(ctx context.Context, job entity.Job) (json.RawMessage, error) {
	var payload jobpayload.DeleteFilePayload
	if err := json.Unmarshal(*job.Payload, &payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal job payload: %w", err)
	}

	f, err := h.uploadFileRepo.FindByID(ctx, payload.UploadFileID)
	if err != nil {
		return nil, fmt.Errorf("failed to look up upload file: %w", err)
	}
	if f == nil {
		return nil, fmt.Errorf("upload file %q not found", payload.UploadFileID)
	}

	version, err := h.versionRepo.FindByID(ctx, f.VersionID)
	if err != nil {
		return nil, fmt.Errorf("failed to look up version: %w", err)
	}
	if version == nil {
		return nil, fmt.Errorf("version %d not found", f.VersionID)
	}

	aliasName := fmt.Sprintf("%s-synonym-%s", h.indexPrefix, strings.ToLower(version.Name))
	indexName := fmt.Sprintf("%s-%d", aliasName, version.CreatedAt.Unix())

	if err := h.synonymRepo.DeleteByFileID(ctx, indexName, payload.UploadFileID); err != nil {
		return nil, fmt.Errorf("failed to delete synonym records: %w", err)
	}

	if err := h.uploadFileRepo.SoftDelete(ctx, payload.UploadFileID, payload.DeletedBy); err != nil {
		return nil, fmt.Errorf("failed to soft-delete upload file record: %w", err)
	}

	filePath := filepath.Join(h.uploadDir, f.FilePath)
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		log.Ctx(ctx).Warn().Err(err).Str("path", filePath).Msg("failed to remove synonym file from disk")
	}

	return nil, nil
}
