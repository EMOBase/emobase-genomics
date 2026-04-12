package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	ucversion "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/version"
	"github.com/gin-gonic/gin"
)

type versionUseCase interface {
	CreateVersion(ctx context.Context, name string) (*entity.Version, error)
	SetDefaultVersion(ctx context.Context, name string) (*entity.Version, error)
	ListVersions(ctx context.Context, page, pageSize int) (*ucversion.VersionList, error)
}

type VersionHandler struct {
	uc versionUseCase
}

func NewVersionHandler(uc versionUseCase) *VersionHandler {
	return &VersionHandler{uc: uc}
}

func (h *VersionHandler) List(c *gin.Context) {
	page := 1
	pageSize := 20

	if p := c.Query("page"); p != "" {
		if v, err := strconv.Atoi(p); err == nil && v > 0 {
			page = v
		}
	}
	if ps := c.Query("page_size"); ps != "" {
		if v, err := strconv.Atoi(ps); err == nil && v > 0 && v <= 100 {
			pageSize = v
		}
	}

	result, err := h.uc.ListVersions(c.Request.Context(), page, pageSize)
	if err != nil {
		panic(err)
	}

	c.JSON(http.StatusOK, result)
}

func (h *VersionHandler) SetDefault(c *gin.Context) {
	var body struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	v, err := h.uc.SetDefaultVersion(c.Request.Context(), body.Name)
	if err != nil {
		if errors.Is(err, ucversion.ErrVersionNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"message": "version not found"})
			return
		}
		panic(err)
	}

	c.JSON(http.StatusOK, v)
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
