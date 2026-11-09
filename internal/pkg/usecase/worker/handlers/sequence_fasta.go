package handlers

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	"github.com/EMOBase/emobase-genomics/internal/pkg/indexname"
	"github.com/EMOBase/emobase-genomics/internal/pkg/jobpayload"
)

type ISequenceUseCase interface {
	Load(ctx context.Context, f io.Reader, indexName, sequenceType, species, fileID string) error
}

type ISequenceRepository interface {
	SetAlias(ctx context.Context, indexName, aliasName, assemblyKey string) error
	DeleteReplaced(ctx context.Context, indexName, sequenceType string, replacedFileIDs []string) error
}

type sequenceFASTAHandler struct {
	versionRepo    IVersionRepository
	sequenceUC     ISequenceUseCase
	sequenceRepo   ISequenceRepository
	uploadFileRepo IReplacedFileRepository
	sequenceType   string
	indexPrefix    string
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

	if payload.AssemblyVersionID == 0 {
		return nil, fmt.Errorf("job payload has no assembly_version_id")
	}
	assemblyKey := entity.FormatAssemblyID(payload.VersionID, payload.AssemblyVersionID)
	aliasName := fmt.Sprintf("%s-sequence-%s", h.indexPrefix, indexname.FromVersionName(version.Name))
	// Use version.CreatedAt.Unix() so all sequence files (RNA, CDS, protein) for the same
	// assembly share one index. Using time.Now() caused each upload to displace the previous.
	indexName := fmt.Sprintf("%s-%s-%d", aliasName, assemblyKey, version.CreatedAt.Unix())

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

	if err := h.sequenceUC.Load(ctx, gr, indexName, h.sequenceType, payload.Species, payload.UploadFileID); err != nil {
		return nil, err
	}

	return nil, h.sequenceRepo.SetAlias(ctx, indexName, aliasName, assemblyKey)
}

// OnComplete removes the sequences of the files this upload replaced, now that
// its own are indexed. An assembly keeps one file per sequence type, but all of
// its sequence files share one index, and a new file rewrites only the
// sequences it still has, so a replaced file's other sequences would otherwise
// stay searchable.
func (h *sequenceFASTAHandler) OnComplete(ctx context.Context, job entity.Job, _ json.RawMessage) error {
	var payload jobpayload.ProcessPayload
	if err := json.Unmarshal(*job.Payload, &payload); err != nil {
		return fmt.Errorf("failed to unmarshal job payload: %w", err)
	}
	version, err := h.versionRepo.FindByID(ctx, payload.VersionID)
	if err != nil || version == nil {
		return fmt.Errorf("failed to look up version %d: %v", payload.VersionID, err)
	}
	replaced, err := h.uploadFileRepo.ListIDsReplacedBy(ctx, payload.UploadFileID)
	if err != nil {
		return fmt.Errorf("failed to list replaced files: %w", err)
	}

	assemblyKey := entity.FormatAssemblyID(payload.VersionID, payload.AssemblyVersionID)
	aliasName := fmt.Sprintf("%s-sequence-%s", h.indexPrefix, indexname.FromVersionName(version.Name))
	indexName := fmt.Sprintf("%s-%s-%d", aliasName, assemblyKey, version.CreatedAt.Unix())
	if err := h.sequenceRepo.DeleteReplaced(ctx, indexName, h.sequenceType, replaced); err != nil {
		return fmt.Errorf("failed to delete replaced sequences from %s: %w", indexName, err)
	}
	return nil
}
