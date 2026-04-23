package handlers

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	"github.com/EMOBase/emobase-genomics/internal/pkg/jobpayload"
	"github.com/rs/zerolog/log"
)

type IOrthologyUseCase interface {
	Load(ctx context.Context, f io.Reader, indexName, fileID, order, algorithm string) error
}

type IOrthologyRepository interface {
	SetAlias(ctx context.Context, indexName, aliasName string) error
	DeleteByFileID(ctx context.Context, indexName, fileID string) error
}

type OrthologyTSVHandler struct {
	versionRepo   IVersionRepository
	orthologyUC   IOrthologyUseCase
	orthologyRepo IOrthologyRepository
}

func NewOrthologyTSVHandler(
	versionRepo IVersionRepository,
	orthologyUC IOrthologyUseCase,
	orthologyRepo IOrthologyRepository,
) *OrthologyTSVHandler {
	return &OrthologyTSVHandler{
		versionRepo:   versionRepo,
		orthologyUC:   orthologyUC,
		orthologyRepo: orthologyRepo,
	}
}

func (h *OrthologyTSVHandler) Handle(ctx context.Context, job entity.Job) error {
	var payload jobpayload.ProcessPayload
	if err := json.Unmarshal(*job.Payload, &payload); err != nil {
		return fmt.Errorf("failed to unmarshal job payload: %w", err)
	}

	version, err := h.versionRepo.FindByID(ctx, payload.VersionID)
	if err != nil {
		return fmt.Errorf("failed to look up version: %w", err)
	}
	if version == nil {
		return fmt.Errorf("version %d not found", payload.VersionID)
	}

	aliasName := fmt.Sprintf("emobasegenomics-orthology-%s", strings.ToLower(version.Name))
	// Use version.CreatedAt.Unix() instead of time.Now().Unix() to fix the index name,
	// so multiple orthology files uploaded for the same version will be indexed into the same ES index.
	indexName := fmt.Sprintf("%s-%d", aliasName, version.CreatedAt.Unix())

	f, err := os.Open(payload.FilePath)
	if err != nil {
		return fmt.Errorf("failed to open file %q: %w", payload.FilePath, err)
	}
	defer func() { _ = f.Close() }()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer func() { _ = gr.Close() }()

	if err := h.orthologyUC.Load(ctx, gr, indexName, payload.UploadFileID, payload.Order, payload.Algorithm); err != nil {
		return err
	}

	return h.orthologyRepo.SetAlias(ctx, indexName, aliasName)
}

// OnFailure removes any partially-inserted ES records for the file so the
// index is not left in a dirty state.
func (h *OrthologyTSVHandler) OnFailure(ctx context.Context, job entity.Job, _ error) error {
	var payload jobpayload.ProcessPayload
	if err := json.Unmarshal(*job.Payload, &payload); err != nil {
		log.Ctx(ctx).Warn().Err(err).Msg("failed to unmarshal orthology payload in OnFailure; skipping cleanup")
		return nil
	}

	version, err := h.versionRepo.FindByID(ctx, payload.VersionID)
	if err != nil || version == nil {
		log.Ctx(ctx).Warn().Err(err).Uint64("versionID", payload.VersionID).Msg("failed to look up version in OnFailure; skipping cleanup")
		return nil
	}

	aliasName := fmt.Sprintf("emobasegenomics-orthology-%s", strings.ToLower(version.Name))
	indexName := fmt.Sprintf("%s-%d", aliasName, version.CreatedAt.Unix())

	if err := h.orthologyRepo.DeleteByFileID(ctx, indexName, payload.UploadFileID); err != nil {
		log.Ctx(ctx).Warn().Err(err).
			Str("indexName", indexName).
			Str("fileID", payload.UploadFileID).
			Msg("failed to clean up orthology records after job failure")
	}
	return nil
}
