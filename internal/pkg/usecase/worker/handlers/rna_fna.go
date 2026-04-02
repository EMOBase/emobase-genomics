package handlers

import (
	"context"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

type RNAFNAHandler struct{}

func NewRNAFNAHandler() *RNAFNAHandler {
	return &RNAFNAHandler{}
}

func (h *RNAFNAHandler) Handle(_ context.Context, _ entity.Job) error {
	return nil
}
