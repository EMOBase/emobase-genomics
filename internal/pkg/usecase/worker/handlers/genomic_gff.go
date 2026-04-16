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
	"github.com/rs/zerolog/log"
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
	jobRepo     IJobRepository
}

func NewGenomicGFFHandler(
	versionRepo IVersionRepository,
	genomicUC IGenomicUseCase,
	genomicRepo IGenomicRepository,
	jobRepo IJobRepository,
) *GenomicGFFHandler {
	return &GenomicGFFHandler{
		versionRepo: versionRepo,
		genomicUC:   genomicUC,
		genomicRepo: genomicRepo,
		jobRepo:     jobRepo,
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

	genomicAliasName := fmt.Sprintf("emobasegenomics-genomiclocation-%s", strings.ToLower(version.Name))
	genomicIndexName := fmt.Sprintf("%s-%d", genomicAliasName, time.Now().UnixMilli())

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

	if err := h.genomicUC.Load(ctx, gr, genomicIndexName); err != nil {
		return err
	}

	if err := h.genomicRepo.SetAlias(ctx, genomicIndexName, genomicAliasName); err != nil {
		return err
	}

	if err := tryEnqueueSetupJBrowse2(ctx, h.jobRepo, job.VersionID); err != nil {
		log.Ctx(ctx).Warn().Err(err).Msg("failed to enqueue setup_jbrowse2 after genomic_gff")
	}

	return nil
}
