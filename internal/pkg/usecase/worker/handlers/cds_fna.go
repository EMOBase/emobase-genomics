package handlers

import (
	"context"

	ucsequence "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/sequence"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

type CDSFNAHandler struct {
	sequenceFASTAHandler
}

func NewCDSFNAHandler(
	versionRepo IVersionRepository,
	sequenceUC ISequenceUseCase,
	sequenceRepo ISequenceRepository,
) *CDSFNAHandler {
	return &CDSFNAHandler{sequenceFASTAHandler{
		versionRepo:  versionRepo,
		sequenceUC:   sequenceUC,
		sequenceRepo: sequenceRepo,
		sequenceType: ucsequence.SEQUENCE_TYPE_CDS,
	}}
}

func (h *CDSFNAHandler) Handle(ctx context.Context, job entity.Job) error {
	return h.handle(ctx, job)
}
