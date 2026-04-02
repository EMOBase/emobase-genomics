package handlers

import (
	"context"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

type ProteinFAAHandler struct{}

func NewProteinFAAHandler() *ProteinFAAHandler {
	return &ProteinFAAHandler{}
}

func (h *ProteinFAAHandler) Handle(_ context.Context, _ entity.Job) error {
	return nil
}
