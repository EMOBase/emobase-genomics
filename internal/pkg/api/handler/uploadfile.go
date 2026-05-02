package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/EMOBase/emobase-genomics/internal/pkg/apires"
	"github.com/EMOBase/emobase-genomics/internal/pkg/auth"
	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	ucjob "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/job"
	"github.com/EMOBase/emobase-genomics/internal/pkg/usecase/upload"
	"github.com/gin-gonic/gin"
)

type uploadFileUseCase interface {
	DeleteFile(ctx context.Context, id string, deletedBy string) (*entity.Job, error)
	ListByVersion(ctx context.Context, versionName string) ([]upload.UploadFileSummary, error)
}

type UploadFileHandler struct {
	uc uploadFileUseCase
}

func NewUploadFileHandler(uc uploadFileUseCase) *UploadFileHandler {
	return &UploadFileHandler{uc: uc}
}

func (h *UploadFileHandler) List(c *gin.Context) {
	version := c.Query("version")
	if version == "" {
		apires.Fail(c, http.StatusBadRequest, "version query parameter is required")
		return
	}

	files, err := h.uc.ListByVersion(c.Request.Context(), version)
	if err != nil {
		if errors.Is(err, upload.ErrVersionNotFound) {
			apires.Fail(c, http.StatusNotFound, "version not found")
			return
		}
		panic(err)
	}

	apires.OK(c, files)
}

func (h *UploadFileHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	deletedBy := auth.UsernameFromContext(c.Request.Context())

	job, err := h.uc.DeleteFile(c.Request.Context(), id, deletedBy)
	if err != nil {
		switch {
		case errors.Is(err, upload.ErrUploadFileNotFound):
			apires.Fail(c, http.StatusNotFound, "upload file not found")
		case errors.Is(err, upload.ErrUploadFileNotDeletable):
			apires.Fail(c, http.StatusUnprocessableEntity, err.Error())
		case errors.Is(err, upload.ErrUploadFileDeletePending):
			apires.Fail(c, http.StatusConflict, err.Error())
		default:
			panic(err)
		}
		return
	}

	apires.OK(c, ucjob.JobSummary{
		ID:          job.ID,
		VersionID:   job.VersionID,
		FileID:      job.FileID,
		Type:        job.Type,
		Description: job.Description,
		Status:      job.Status,
		Payload:     job.Payload,
	})
}
