package handlers

import (
	"context"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

type GenomicGFFHandler struct{}

func (h *GenomicGFFHandler) Handle(_ context.Context, _ entity.Job) error {
	return nil
}
