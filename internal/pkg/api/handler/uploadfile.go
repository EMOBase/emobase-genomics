package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/EMOBase/emobase-genomics/internal/pkg/apires"
	"github.com/EMOBase/emobase-genomics/internal/pkg/auth"
	"github.com/EMOBase/emobase-genomics/internal/pkg/usecase/upload"
	"github.com/gin-gonic/gin"
)

type uploadFileUseCase interface {
	DeleteFile(ctx context.Context, id string, deletedBy string) error
}

type UploadFileHandler struct {
	uc uploadFileUseCase
}

func NewUploadFileHandler(uc uploadFileUseCase) *UploadFileHandler {
	return &UploadFileHandler{uc: uc}
}

func (h *UploadFileHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	deletedBy := auth.UsernameFromContext(c.Request.Context())

	if err := h.uc.DeleteFile(c.Request.Context(), id, deletedBy); err != nil {
		switch {
		case errors.Is(err, upload.ErrUploadFileNotFound):
			apires.Fail(c, http.StatusNotFound, "upload file not found")
		case errors.Is(err, upload.ErrUploadFileNotDeletable):
			apires.Fail(c, http.StatusUnprocessableEntity, err.Error())
		default:
			panic(err)
		}
		return
	}

	apires.OK(c, nil)
}
