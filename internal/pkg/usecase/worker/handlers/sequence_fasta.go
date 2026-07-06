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
	indexPrefix  string
}

func (h *sequenceFASTAHandler) handle(ctx context.Context, job entity.Job) (json.RawMessage, error) {
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

	aliasName := fmt.Sprintf("%s-sequence-%s", h.indexPrefix, strings.ToLower(version.Name))
	// Use version.CreatedAt.Unix() so all sequence files (RNA, CDS, protein) for the same
	// version share one index. Using time.Now() caused each upload to displace the previous.
	indexName := fmt.Sprintf("%s-%d", aliasName, version.CreatedAt.Unix())

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

	if err := h.sequenceUC.Load(ctx, gr, indexName, h.sequenceType); err != nil {
		return nil, err
	}

	return nil, h.sequenceRepo.SetAlias(ctx, indexName, aliasName)
}
