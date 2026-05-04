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

type IGenomicUseCase interface {
	Load(ctx context.Context, f io.Reader, indexName string) error
}

type IGenomicRepository interface {
	SetAlias(ctx context.Context, indexName, aliasName string) error
	DeleteStaleIndexes(ctx context.Context, aliasName, liveIndexName string) error
}

type GenomicGFFHandler struct {
	versionRepo IVersionRepository
	genomicUC   IGenomicUseCase
	genomicRepo IGenomicRepository
	indexPrefix string
}

func NewGenomicGFFHandler(
	versionRepo IVersionRepository,
	genomicUC IGenomicUseCase,
	genomicRepo IGenomicRepository,
	indexPrefix string,
) *GenomicGFFHandler {
	return &GenomicGFFHandler{
		versionRepo: versionRepo,
		genomicUC:   genomicUC,
		genomicRepo: genomicRepo,
		indexPrefix: indexPrefix,
	}
}

type genomicGFFResult struct {
	IndexName string `json:"indexName"`
	AliasName string `json:"aliasName"`
}

func (h *GenomicGFFHandler) Handle(ctx context.Context, job entity.Job) (json.RawMessage, error) {
	var payload jobpayload.ProcessPayload
	if err := json.Unmarshal(*job.Payload, &payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal job payload: %w", err)
	}

	version, err := h.versionRepo.FindByID(ctx, payload.VersionID)
	if err != nil {
		return nil, fmt.Errorf("failed to look up version: %w", err)
	}
	if version == nil {
		return nil, fmt.Errorf("version %d not found", payload.VersionID)
	}

	aliasName := fmt.Sprintf("%s-genomiclocation-%s", h.indexPrefix, strings.ToLower(version.Name))
	indexName := fmt.Sprintf("%s-%d", aliasName, time.Now().Unix())

	f, err := os.Open(payload.FilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file %q: %w", payload.FilePath, err)
	}
	defer func() { _ = f.Close() }()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer func() { _ = gr.Close() }()

	if err := h.genomicUC.Load(ctx, gr, indexName); err != nil {
		return nil, err
	}

	if err := h.genomicRepo.SetAlias(ctx, indexName, aliasName); err != nil {
		return nil, err
	}

	raw, err := json.Marshal(genomicGFFResult{IndexName: indexName, AliasName: aliasName})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal result: %w", err)
	}
	return raw, nil
}

func (h *GenomicGFFHandler) OnComplete(ctx context.Context, _ entity.Job, result json.RawMessage) error {
	var res genomicGFFResult
	if err := json.Unmarshal(result, &res); err != nil {
		log.Ctx(ctx).Warn().Err(err).Msg("failed to unmarshal genomic_gff result in OnComplete; skipping stale index cleanup")
		return nil
	}

	if err := h.genomicRepo.DeleteStaleIndexes(ctx, res.AliasName, res.IndexName); err != nil {
		log.Ctx(ctx).Warn().Err(err).
			Str("aliasName", res.AliasName).
			Str("liveIndex", res.IndexName).
			Msg("failed to delete stale genomic indexes")
	}
	return nil
}
