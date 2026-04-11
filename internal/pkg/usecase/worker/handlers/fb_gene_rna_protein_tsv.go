package handlers

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	"github.com/EMOBase/emobase-genomics/internal/pkg/jobpayload"
	synonymparser "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/synonym/parser"
)

type FBGNFBTRFBPPTSVHandler struct {
	synonymUC   ISynonymUseCase
	synonymRepo ISynonymRepository
	parser      synonymparser.ISynonymParser
}

func NewFBGNFBTRFBPPTSVHandler(
	synonymUC ISynonymUseCase,
	synonymRepo ISynonymRepository,
	parser synonymparser.ISynonymParser,
) *FBGNFBTRFBPPTSVHandler {
	return &FBGNFBTRFBPPTSVHandler{
		synonymUC:   synonymUC,
		synonymRepo: synonymRepo,
		parser:      parser,
	}
}

func (h *FBGNFBTRFBPPTSVHandler) Handle(ctx context.Context, job entity.Job) error {
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
