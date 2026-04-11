package handlers

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	"github.com/EMOBase/emobase-genomics/internal/pkg/jobpayload"
	synonymparser "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/synonym/parser"
)

type ISynonymUseCase interface {
	Load(ctx context.Context, f io.Reader, indexName string, p synonymparser.ISynonymParser) error
}

type ISynonymRepository interface {
	SetAlias(ctx context.Context, indexName, aliasName string) error
}

type FBSynonymTSVHandler struct {
	synonymUC   ISynonymUseCase
	synonymRepo ISynonymRepository
	parser      synonymparser.ISynonymParser
}

func NewFBSynonymTSVHandler(
	synonymUC ISynonymUseCase,
	synonymRepo ISynonymRepository,
	parser synonymparser.ISynonymParser,
) *FBSynonymTSVHandler {
	return &FBSynonymTSVHandler{
		synonymUC:   synonymUC,
		synonymRepo: synonymRepo,
		parser:      parser,
	}
}

func (h *FBSynonymTSVHandler) Handle(ctx context.Context, job entity.Job) error {
	var payload jobpayload.ProcessPayload
	if err := json.Unmarshal(*job.Payload, &payload); err != nil {
		return fmt.Errorf("failed to unmarshal job payload: %w", err)
	}

	aliasName := payload.SynonymAliasName
	indexName := payload.SynonymIndexName

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

	if err := h.synonymUC.Load(ctx, gr, indexName, h.parser); err != nil {
		return err
	}

	return h.synonymRepo.SetAlias(ctx, indexName, aliasName)
}
