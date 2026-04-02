package handlers

import (
	"context"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

type CDSFNAHandler struct{}

func NewCDSFNAHandler() *CDSFNAHandler {
	return &CDSFNAHandler{}
}

func (h *CDSFNAHandler) Handle(_ context.Context, _ entity.Job) error {
	return nil
}
