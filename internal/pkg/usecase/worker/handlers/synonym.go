package handlers

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

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

type SynonymHandler struct {
	versionRepo IVersionRepository
	synonymUC   ISynonymUseCase
	synonymRepo ISynonymRepository
	gff3Parser  synonymparser.ISynonymParser
	fbSynParser synonymparser.ISynonymParser
	fbGRPParser synonymparser.ISynonymParser
}

func NewSynonymHandler(
	versionRepo IVersionRepository,
	synonymUC ISynonymUseCase,
	synonymRepo ISynonymRepository,
	gff3Parser synonymparser.ISynonymParser,
	fbSynParser synonymparser.ISynonymParser,
	fbGRPParser synonymparser.ISynonymParser,
) *SynonymHandler {
	return &SynonymHandler{
		versionRepo: versionRepo,
		synonymUC:   synonymUC,
		synonymRepo: synonymRepo,
		gff3Parser:  gff3Parser,
		fbSynParser: fbSynParser,
		fbGRPParser: fbGRPParser,
	}
}

func (h *SynonymHandler) Handle(ctx context.Context, job entity.Job) error {
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

	aliasName := fmt.Sprintf("emobasegenomics-synonym-%s", strings.ToLower(version.Name))
	indexName := fmt.Sprintf("%s-%d", aliasName, time.Now().UnixMilli())

	// Load synonyms from the GFF3 file.
	if err := h.loadGzip(ctx, payload.FilePath, indexName, h.gff3Parser); err != nil {
		return err
	}

	// Load synonyms from each versionless synonym file using the appropriate parser.
	for _, path := range payload.SynonymFiles {
		parser := h.parserForFile(path)
		if parser == nil {
			continue
		}
		if err := h.loadGzip(ctx, path, indexName, parser); err != nil {
			return err
		}
	}

	return h.synonymRepo.SetAlias(ctx, indexName, aliasName)
}

func (h *SynonymHandler) parserForFile(path string) synonymparser.ISynonymParser {
	base := filepath.Base(path)
	switch {
	case strings.HasPrefix(base, "fb_synonym_"):
		return h.fbSynParser
	case strings.HasPrefix(base, "fbgn_fbtr_fbpp_"):
		return h.fbGRPParser
	default:
		return nil
	}
}

func (h *SynonymHandler) loadGzip(ctx context.Context, path, indexName string, parser synonymparser.ISynonymParser) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open file %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader for %q: %w", path, err)
	}
	defer func() { _ = gr.Close() }()

	return h.synonymUC.Load(ctx, gr, indexName, parser)
}
