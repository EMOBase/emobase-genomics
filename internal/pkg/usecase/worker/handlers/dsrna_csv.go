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

type IDsRNAUseCase interface {
	Load(ctx context.Context, r io.Reader, indexName string) error
}

type IDsRNARepository interface {
	SetAlias(ctx context.Context, indexName, aliasName string) error
	DeleteStaleIndexes(ctx context.Context, aliasName, liveIndexName string) error
}

type DsRNACSVHandler struct {
	versionRepo IVersionRepository
	dsrnaUC     IDsRNAUseCase
	dsrnaRepo   IDsRNARepository
	indexPrefix string
}

func NewDsRNACSVHandler(
	versionRepo IVersionRepository,
	dsrnaUC IDsRNAUseCase,
	dsrnaRepo IDsRNARepository,
	indexPrefix string,
) *DsRNACSVHandler {
	return &DsRNACSVHandler{
		versionRepo: versionRepo,
		dsrnaUC:     dsrnaUC,
		dsrnaRepo:   dsrnaRepo,
		indexPrefix: indexPrefix,
	}
}

type dsrnaCSVResult struct {
	IndexName string `json:"indexName"`
	AliasName string `json:"aliasName"`
}

func (h *DsRNACSVHandler) Handle(ctx context.Context, job entity.Job) (json.RawMessage, error) {
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

	aliasName := fmt.Sprintf("%s-dsrna-%s", h.indexPrefix, strings.ToLower(version.Name))
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

	if err := h.dsrnaUC.Load(ctx, gr, indexName); err != nil {
		return nil, err
	}

	if err := h.dsrnaRepo.SetAlias(ctx, indexName, aliasName); err != nil {
		return nil, err
	}

	raw, err := json.Marshal(dsrnaCSVResult{IndexName: indexName, AliasName: aliasName})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal result: %w", err)
	}
	return raw, nil
}

func (h *DsRNACSVHandler) OnComplete(ctx context.Context, _ entity.Job, result json.RawMessage) error {
	var res dsrnaCSVResult
	if err := json.Unmarshal(result, &res); err != nil {
		log.Ctx(ctx).Warn().Err(err).Msg("failed to unmarshal dsrna_csv result in OnComplete; skipping stale index cleanup")
		return nil
	}

	if err := h.dsrnaRepo.DeleteStaleIndexes(ctx, res.AliasName, res.IndexName); err != nil {
		log.Ctx(ctx).Warn().Err(err).
			Str("aliasName", res.AliasName).
			Str("liveIndex", res.IndexName).
			Msg("failed to delete stale dsrna indexes")
	}
	return nil
}
