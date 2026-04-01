package handlers

import (
	"context"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

type OrthologyTSVHandler struct{}

func (h *OrthologyTSVHandler) Handle(_ context.Context, _ entity.Job) error {
	return nil
}
