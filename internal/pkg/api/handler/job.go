package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/EMOBase/emobase-genomics/internal/pkg/apires"
	ucjob "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/job"
	"github.com/gin-gonic/gin"
)

type jobUseCase interface {
	ListJobsByVersion(ctx context.Context, versionName string) ([]ucjob.JobSummary, error)
	GetJob(ctx context.Context, id uint64) (*ucjob.JobDetail, error)
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
		if errors.Is(err, ucjob.ErrVersionNotFound) {
			apires.Fail(c, http.StatusNotFound, "version not found")
			return
		}
		panic(err)
	}

	apires.OK(c, summaries)
}

func (h *JobHandler) Get(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		apires.Fail(c, http.StatusBadRequest, "job id must be an integer")
		return
	}

	job, err := h.uc.GetJob(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, ucjob.ErrJobNotFound) {
			apires.Fail(c, http.StatusNotFound, "job not found")
			return
		}
		panic(err)
	}

	apires.OK(c, job)
}
