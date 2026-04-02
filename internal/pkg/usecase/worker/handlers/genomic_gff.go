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

type IVersionRepository interface {
	FindByID(ctx context.Context, id uint64) (*entity.Version, error)
}

type IGenomicUseCase interface {
	Load(ctx context.Context, f io.Reader, indexName string) error
}

type IGenomicRepository interface {
	SetAlias(ctx context.Context, indexName, aliasName string) error
}

type GenomicGFFHandler struct {
	versionRepo IVersionRepository
	genomicUC   IGenomicUseCase
	genomicRepo IGenomicRepository
}

func NewGenomicGFFHandler(
	versionRepo IVersionRepository,
	genomicUC IGenomicUseCase,
	genomicRepo IGenomicRepository,
) *GenomicGFFHandler {
	return &GenomicGFFHandler{
		versionRepo: versionRepo,
		genomicUC:   genomicUC,
		genomicRepo: genomicRepo,
	}
}

func (h *GenomicGFFHandler) Handle(ctx context.Context, job entity.Job) error {
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

	aliasName := fmt.Sprintf("emobasegenomics-genomiclocation-%s", strings.ToLower(version.Name))
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

	if err := h.genomicUC.Load(ctx, gr, indexName); err != nil {
		return err
	}

	return h.genomicRepo.SetAlias(ctx, indexName, aliasName)
}
