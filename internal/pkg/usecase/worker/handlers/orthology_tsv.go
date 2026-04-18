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

type IOrthologyUseCase interface {
	Load(ctx context.Context, f io.Reader, indexName, order, algorithm string) error
}

type IOrthologyRepository interface {
	SetAlias(ctx context.Context, indexName, aliasName string) error
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
	indexName := fmt.Sprintf("%s-%d", aliasName, time.Now().UnixMilli())

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

	if err := h.orthologyUC.Load(ctx, gr, indexName, payload.Order, payload.Algorithm); err != nil {
		return err
	}

	return h.orthologyRepo.SetAlias(ctx, indexName, aliasName)
}
