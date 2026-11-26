package handlers

import (
	"context"
	"encoding/json"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

// RestartBlastHandler restarts the BLAST server container, so it stops serving
// databases that were removed outside a release.
type RestartBlastHandler struct {
	containerName string
}

func NewRestartBlastHandler(containerName string) *RestartBlastHandler {
	return &RestartBlastHandler{containerName: containerName}
}

func (h *RestartBlastHandler) Handle(ctx context.Context, _ entity.Job) (json.RawMessage, error) {
	if h.containerName == "" {
		return nil, nil
	}
	return nil, restartDockerContainer(ctx, h.containerName)
}
