package handlers

import (
	"context"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

type GenomicFNAHandler struct{}

func NewGenomicFNAHandler() *GenomicFNAHandler {
	return &GenomicFNAHandler{}
}

func (h *GenomicFNAHandler) Handle(_ context.Context, _ entity.Job) error {
	return nil
}
