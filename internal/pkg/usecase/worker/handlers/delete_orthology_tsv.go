package handlers

import (
	"context"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

type DeleteOrthologyTSVHandler struct{}

func NewDeleteOrthologyTSVHandler() *DeleteOrthologyTSVHandler {
	return &DeleteOrthologyTSVHandler{}
}

func (h *DeleteOrthologyTSVHandler) Handle(_ context.Context, _ entity.Job) error {
	return nil
}
