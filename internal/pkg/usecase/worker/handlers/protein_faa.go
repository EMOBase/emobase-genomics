package handlers

import (
	"context"

	ucsequence "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/sequence"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

type ProteinFAAHandler struct {
	sequenceFASTAHandler
}

func NewProteinFAAHandler(
	versionRepo IVersionRepository,
	sequenceUC ISequenceUseCase,
	sequenceRepo ISequenceRepository,
) *ProteinFAAHandler {
	return &ProteinFAAHandler{sequenceFASTAHandler{
		versionRepo:  versionRepo,
		sequenceUC:   sequenceUC,
		sequenceRepo: sequenceRepo,
		sequenceType: ucsequence.SEQUENCE_TYPE_PROTEIN,
	}}
}

func (h *ProteinFAAHandler) Handle(ctx context.Context, job entity.Job) error {
	return h.handle(ctx, job)
}
