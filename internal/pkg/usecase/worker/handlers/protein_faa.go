package handlers

import (
	"context"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	ucsequence "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/sequence"
	ucworker "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/worker"
)

type ProteinFAAHandler struct {
	sequenceFASTAHandler
	jobRepo IJobRepository
}

func NewProteinFAAHandler(
	versionRepo IVersionRepository,
	sequenceUC ISequenceUseCase,
	sequenceRepo ISequenceRepository,
	jobRepo IJobRepository,
) *ProteinFAAHandler {
	return &ProteinFAAHandler{
		sequenceFASTAHandler: sequenceFASTAHandler{
			versionRepo:  versionRepo,
			sequenceUC:   sequenceUC,
			sequenceRepo: sequenceRepo,
			sequenceType: ucsequence.SEQUENCE_TYPE_PROTEIN,
		},
		jobRepo: jobRepo,
	}
}

func (h *ProteinFAAHandler) Handle(ctx context.Context, job entity.Job) error {
	if err := h.handle(ctx, job); err != nil {
		return err
	}
	return enqueueSetupBlastJob(ctx, h.jobRepo, job, ucworker.JobTypeProteinFAASetupBlast)
}
