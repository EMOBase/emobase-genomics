package handlers

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	"github.com/EMOBase/emobase-genomics/internal/pkg/jobpayload"
)

type ISequenceUseCase interface {
	Load(ctx context.Context, f io.Reader, indexName, sequenceType string) error
}

type ISequenceRepository interface {
	SetAlias(ctx context.Context, indexName, aliasName string) error
}

type sequenceFASTAHandler struct {
	versionRepo  IVersionRepository
	sequenceUC   ISequenceUseCase
	sequenceRepo ISequenceRepository
	sequenceType string
}

func (h *sequenceFASTAHandler) handle(ctx context.Context, job entity.Job) error {
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

	aliasName := fmt.Sprintf("emobasegenomics-sequence-%s", strings.ToLower(version.Name))
	indexName := fmt.Sprintf("%s-%d", aliasName, time.Now().UnixMilli())

	f, err := os.Open(payload.FilePath)
	if err != nil {
		return fmt.Errorf("failed to open file %q: %w", payload.FilePath, err)
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gr.Close()

	if err := h.sequenceUC.Load(ctx, gr, indexName, h.sequenceType); err != nil {
		return err
	}

	return h.sequenceRepo.SetAlias(ctx, indexName, aliasName)
}
