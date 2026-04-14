package handlers

import (
	"context"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	ucsequence "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/sequence"
	ucworker "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/worker"
)

type RNAFNAHandler struct {
	sequenceFASTAHandler
	jobRepo IJobRepository
}

func NewRNAFNAHandler(
	versionRepo IVersionRepository,
	sequenceUC ISequenceUseCase,
	sequenceRepo ISequenceRepository,
	jobRepo IJobRepository,
) *RNAFNAHandler {
	return &RNAFNAHandler{
		sequenceFASTAHandler: sequenceFASTAHandler{
			versionRepo:  versionRepo,
			sequenceUC:   sequenceUC,
			sequenceRepo: sequenceRepo,
			sequenceType: ucsequence.SEQUENCE_TYPE_TRANSCRIPT,
		},
		jobRepo: jobRepo,
	}
}

func (h *RNAFNAHandler) Handle(ctx context.Context, job entity.Job) error {
	if err := h.handle(ctx, job); err != nil {
		return err
	}
	return enqueueSetupBlastJob(ctx, h.jobRepo, job, ucworker.JobTypeRNAFNASetupBlast)
}
