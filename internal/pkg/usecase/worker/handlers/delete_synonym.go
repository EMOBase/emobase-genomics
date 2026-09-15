package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	"github.com/EMOBase/emobase-genomics/internal/pkg/indexname"
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
	jobRepo        IJobRepository
	synonymRepo    IDeleteSynonymRepository
	indexPrefix    string
}

func NewDeleteSynonymHandler(
	uploadDir string,
	uploadFileRepo IDeleteUploadFileRepository,
	versionRepo IVersionRepository,
	jobRepo IJobRepository,
	synonymRepo IDeleteSynonymRepository,
	indexPrefix string,
) *DeleteSynonymHandler {
	return &DeleteSynonymHandler{
		uploadDir:      uploadDir,
		uploadFileRepo: uploadFileRepo,
		versionRepo:    versionRepo,
		jobRepo:        jobRepo,
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

	species, err := h.synonymSpeciesForFile(ctx, payload.UploadFileID)
	if err != nil {
		return nil, err
	}

	aliasName := fmt.Sprintf("%s-synonym-%s", h.indexPrefix, indexname.FromVersionName(version.Name))
	indexName := fmt.Sprintf("%s-%s-%d", aliasName, indexname.FromSpecies(species), version.CreatedAt.Unix())

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

// synonymSpeciesForFile recovers the species this file's SPECIES.SYNONYM job
// was indexed under from that job's own payload — the upload_files row itself
// does not carry it, and it may differ from the file's owning Assembly
// Version's species (a synonym file can describe a different species than
// the assembly it's filed under).
func (h *DeleteSynonymHandler) synonymSpeciesForFile(ctx context.Context, uploadFileID string) (string, error) {
	j, err := h.jobRepo.FindLatestByFileAndType(ctx, uploadFileID, entity.JobTypeSpeciesSynonym)
	if err != nil {
		return "", fmt.Errorf("failed to look up %s job for file: %w", entity.JobTypeSpeciesSynonym, err)
	}
	if j == nil || j.Payload == nil {
		return "", fmt.Errorf("no %s job found for file %q", entity.JobTypeSpeciesSynonym, uploadFileID)
	}
	var p jobpayload.SpeciesSynonymPayload
	if err := json.Unmarshal(*j.Payload, &p); err != nil {
		return "", fmt.Errorf("failed to unmarshal %s payload: %w", entity.JobTypeSpeciesSynonym, err)
	}
	return p.Species, nil
}
