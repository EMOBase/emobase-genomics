package handlers

import (
	"context"
	"encoding/json"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	ucsequence "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/sequence"
)

type RNAFNAHandler struct {
	sequenceFASTAHandler
}

func NewRNAFNAHandler(
	versionRepo IVersionRepository,
	sequenceUC ISequenceUseCase,
	sequenceRepo ISequenceRepository,
) *RNAFNAHandler {
	return &RNAFNAHandler{
		sequenceFASTAHandler: sequenceFASTAHandler{
			versionRepo:  versionRepo,
			sequenceUC:   sequenceUC,
			sequenceRepo: sequenceRepo,
			sequenceType: ucsequence.SEQUENCE_TYPE_TRANSCRIPT,
		},
	}
}

func (h *RNAFNAHandler) Handle(ctx context.Context, job entity.Job) (json.RawMessage, error) {
	return h.handle(ctx, job)
}
