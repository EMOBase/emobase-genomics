package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	ucjob "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/job"
	"github.com/gin-gonic/gin"
)

type jobUseCase interface {
	GetJob(ctx context.Context, id uint64) (*entity.Job, error)
}

type JobHandler struct {
	uc jobUseCase
}

func NewJobHandler(uc jobUseCase) *JobHandler {
	return &JobHandler{uc: uc}
}

func (h *JobHandler) Get(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid job id"})
		return
	}

	job, err := h.uc.GetJob(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, ucjob.ErrJobNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"message": "job not found"})
			return
		}
		panic(err)
	}

	c.JSON(http.StatusOK, gin.H{
		"id":              job.ID,
		"status":          job.Status,
		"result_metadata": job.ResultMetadata,
	})
}
