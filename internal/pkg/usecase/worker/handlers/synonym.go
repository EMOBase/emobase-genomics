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

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	"github.com/EMOBase/emobase-genomics/internal/pkg/jobpayload"
	synonymparser "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/synonym/parser"
	"github.com/rs/zerolog/log"
)

type ISynonymUseCase interface {
	Load(ctx context.Context, f io.Reader, indexName, fileID string, p synonymparser.ISynonymParser) error
}

type ISynonymRepository interface {
	SetAlias(ctx context.Context, indexName, aliasName string) error
	DeleteByFileID(ctx context.Context, indexName, fileID string) error
}

type SynonymHandler struct {
	versionRepo IVersionRepository
	synonymUC   ISynonymUseCase
	synonymRepo ISynonymRepository
	indexPrefix string
}

func NewSynonymHandler(
	versionRepo IVersionRepository,
	synonymUC ISynonymUseCase,
	synonymRepo ISynonymRepository,
	indexPrefix string,
) *SynonymHandler {
	return &SynonymHandler{
		versionRepo: versionRepo,
		synonymUC:   synonymUC,
		synonymRepo: synonymRepo,
		indexPrefix: indexPrefix,
	}
}

func (h *SynonymHandler) Handle(ctx context.Context, job entity.Job) (json.RawMessage, error) {
	var payload jobpayload.SpeciesSynonymPayload
	if err := json.Unmarshal(*job.Payload, &payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal %s payload: %w", entity.JobTypeSpeciesSynonym, err)
	}

	version, err := h.versionRepo.FindByID(ctx, payload.VersionID)
	if err != nil {
		return nil, fmt.Errorf("failed to look up version: %w", err)
	}
	if version == nil {
		return nil, fmt.Errorf("version %d not found", payload.VersionID)
	}

	aliasName := fmt.Sprintf("%s-synonym-%s", h.indexPrefix, strings.ToLower(version.Name))
	// Use version.CreatedAt.Unix() instead of time.Now().Unix() to fix the index name,
	// so multiple synonym files uploaded for the same version will be indexed into the same ES index.
	indexName := fmt.Sprintf("%s-%d", aliasName, version.CreatedAt.Unix())

	parser := h.parserForFile(payload)
	if parser == nil {
		return nil, fmt.Errorf("unrecognised synonym file: %q", filepath.Base(payload.FilePath))
	}

	if err := h.loadGzip(ctx, payload.FilePath, indexName, payload.UploadFileID, parser); err != nil {
		return nil, err
	}

	if err := h.synonymRepo.SetAlias(ctx, indexName, aliasName); err != nil {
		return nil, err
	}

	return nil, nil
}

// parserForFile selects the appropriate parser based on filename prefix/extension.
// *.gff.gz / *.gff3.gz  → GFF3SynonymParser             (any species)
// fb_synonym_*           → FlyBaseSynonymParser          (Dmel fb_synonym files)
// fbgn_fbtr_fbpp_*       → FlyBaseGeneRNAProteinMapParser (Dmel gene/RNA/protein map)
// ib_tc*                 → IBTCParser                    (Tcas iB-to-TC mapping)
func (h *SynonymHandler) parserForFile(payload jobpayload.SpeciesSynonymPayload) synonymparser.ISynonymParser {
	base := strings.ToLower(filepath.Base(payload.FilePath))
	switch {
	case strings.HasSuffix(base, ".gff.gz") || strings.HasSuffix(base, ".gff3.gz"):
		return synonymparser.NewGFF3SynonymParser(payload.Species, payload.GeneIDKey, payload.TrimPrefixChars, payload.TrimSuffixChars, payload.OldGeneIDKeys)
	case strings.HasPrefix(base, "fb_synonym_"):
		return synonymparser.NewFlyBaseSynonymParser(payload.Species)
	case strings.HasPrefix(base, "fbgn_fbtr_fbpp_"):
		return synonymparser.NewFlyBaseGeneRNAProteinMapParser(payload.Species)
	case strings.HasPrefix(base, "ib_tc"):
		return synonymparser.NewIBTCParser(payload.Species)
	default:
		return nil
	}
}

// OnFailure removes any partially-inserted ES records for the file so the index
// is not left in a dirty state.
func (h *SynonymHandler) OnFailure(ctx context.Context, job entity.Job, _ error) error {
	var payload jobpayload.SpeciesSynonymPayload
	if err := json.Unmarshal(*job.Payload, &payload); err != nil {
		log.Ctx(ctx).Warn().Err(err).Msg("failed to unmarshal synonym payload in OnFailure; skipping cleanup")
		return nil
	}

	version, err := h.versionRepo.FindByID(ctx, payload.VersionID)
	if err != nil || version == nil {
		log.Ctx(ctx).Warn().Err(err).Uint64("versionID", payload.VersionID).Msg("failed to look up version in OnFailure; skipping cleanup")
		return nil
	}

	aliasName := fmt.Sprintf("%s-synonym-%s", h.indexPrefix, strings.ToLower(version.Name))
	indexName := fmt.Sprintf("%s-%d", aliasName, version.CreatedAt.Unix())

	if err := h.synonymRepo.DeleteByFileID(ctx, indexName, payload.UploadFileID); err != nil {
		log.Ctx(ctx).Warn().Err(err).
			Str("indexName", indexName).
			Str("fileID", payload.UploadFileID).
			Msg("failed to clean up synonym records after job failure")
	}
	return nil
}

func (h *SynonymHandler) loadGzip(ctx context.Context, path, indexName, fileID string, parser synonymparser.ISynonymParser) error {
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

	return h.synonymUC.Load(ctx, gr, indexName, fileID, parser)
}
