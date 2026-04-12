package handler

import (
	"context"
	"net/http"

	"github.com/EMOBase/emobase-genomics/internal/pkg/apires"
	ucjob "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/job"
	"github.com/gin-gonic/gin"
)

type jobUseCase interface {
	ListJobsByVersion(ctx context.Context, versionName string) ([]ucjob.JobSummary, error)
}

type JobHandler struct {
	uc jobUseCase
}

func NewJobHandler(uc jobUseCase) *JobHandler {
	return &JobHandler{uc: uc}
}

func (h *JobHandler) ListByVersion(c *gin.Context) {
	version := c.Query("version")
	if version == "" {
		apires.Fail(c, http.StatusBadRequest, "version query parameter is required")
		return
	}

	summaries, err := h.uc.ListJobsByVersion(c.Request.Context(), version)
	if err != nil {
		panic(err)
	}

	apires.OK(c, summaries)
}
