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
	synonymparser "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/synonym/parser"
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
	synonymUC   ISynonymUseCase
	synonymRepo ISynonymRepository
	gff3Parser  synonymparser.ISynonymParser
}

func NewGenomicGFFHandler(
	versionRepo IVersionRepository,
	genomicUC IGenomicUseCase,
	genomicRepo IGenomicRepository,
	synonymUC ISynonymUseCase,
	synonymRepo ISynonymRepository,
	gff3Parser synonymparser.ISynonymParser,
) *GenomicGFFHandler {
	return &GenomicGFFHandler{
		versionRepo: versionRepo,
		genomicUC:   genomicUC,
		genomicRepo: genomicRepo,
		synonymUC:   synonymUC,
		synonymRepo: synonymRepo,
		gff3Parser:  gff3Parser,
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

	// --- Genomic locations ---
	if err := h.loadGzipFile(payload.FilePath, func(r io.Reader) error {
		return h.genomicUC.Load(ctx, r, genomicIndexName)
	}); err != nil {
		return err
	}

	if err := h.genomicRepo.SetAlias(ctx, genomicIndexName, genomicAliasName); err != nil {
		return err
	}

	// --- Synonyms from GFF3 (second pass over the same file) ---
	if err := h.loadGzipFile(payload.FilePath, func(r io.Reader) error {
		return h.synonymUC.Load(ctx, r, payload.SynonymIndexName, h.gff3Parser)
	}); err != nil {
		return err
	}

	return h.synonymRepo.SetAlias(ctx, payload.SynonymIndexName, payload.SynonymAliasName)
}

func (h *GenomicGFFHandler) loadGzipFile(path string, fn func(io.Reader) error) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open file %q: %w", path, err)
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gr.Close()

	return fn(gr)
}
