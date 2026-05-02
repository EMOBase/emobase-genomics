package handlers

import (
	"context"
	"encoding/json"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	ucsequence "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/sequence"
)

type ProteinFAAHandler struct {
	sequenceFASTAHandler
}

func NewProteinFAAHandler(
	versionRepo IVersionRepository,
	sequenceUC ISequenceUseCase,
	sequenceRepo ISequenceRepository,
) *ProteinFAAHandler {
	return &ProteinFAAHandler{
		sequenceFASTAHandler: sequenceFASTAHandler{
			versionRepo:  versionRepo,
			sequenceUC:   sequenceUC,
			sequenceRepo: sequenceRepo,
			sequenceType: ucsequence.SEQUENCE_TYPE_PROTEIN,
		},
	}
}

func (h *ProteinFAAHandler) Handle(ctx context.Context, job entity.Job) (json.RawMessage, error) {
	return h.handle(ctx, job)
}
