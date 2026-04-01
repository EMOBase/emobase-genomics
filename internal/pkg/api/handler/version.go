package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	ucversion "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/version"
	"github.com/gin-gonic/gin"
)

type versionUseCase interface {
	CreateVersion(ctx context.Context, name string) (*entity.Version, error)
}

type VersionHandler struct {
	uc versionUseCase
}

func NewVersionHandler(uc versionUseCase) *VersionHandler {
	return &VersionHandler{uc: uc}
}

func (h *VersionHandler) Create(c *gin.Context) {
	var body struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	v, err := h.uc.CreateVersion(c.Request.Context(), body.Name)
	if err != nil {
		if errors.Is(err, ucversion.ErrVersionAlreadyExists) {
			c.JSON(http.StatusBadRequest, gin.H{"message": "version already exists"})
			return
		}
		panic(err)
	}

	c.JSON(http.StatusCreated, v)
}
