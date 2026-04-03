package handlers

import (
	"context"

	ucsequence "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/sequence"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

type RNAFNAHandler struct {
	sequenceFASTAHandler
}

func NewRNAFNAHandler(
	versionRepo IVersionRepository,
	sequenceUC ISequenceUseCase,
	sequenceRepo ISequenceRepository,
) *RNAFNAHandler {
	return &RNAFNAHandler{sequenceFASTAHandler{
		versionRepo:  versionRepo,
		sequenceUC:   sequenceUC,
		sequenceRepo: sequenceRepo,
		sequenceType: ucsequence.SEQUENCE_TYPE_TRANSCRIPT,
	}}
}

func (h *RNAFNAHandler) Handle(ctx context.Context, job entity.Job) error {
	return h.handle(ctx, job)
}
