package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/EMOBase/emobase-genomics/internal/pkg/apires"
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

	apires.OK(c, result)
}

func (h *VersionHandler) SetDefault(c *gin.Context) {
	var body struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		apires.Fail(c, http.StatusBadRequest, err.Error())
		return
	}

	v, err := h.uc.SetDefaultVersion(c.Request.Context(), body.Name)
	if err != nil {
		if errors.Is(err, ucversion.ErrVersionNotFound) {
			apires.Fail(c, http.StatusNotFound, "version not found")
			return
		}
		panic(err)
	}

	apires.OK(c, v)
}

func (h *VersionHandler) Create(c *gin.Context) {
	var body struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		apires.Fail(c, http.StatusBadRequest, err.Error())
		return
	}

	v, err := h.uc.CreateVersion(c.Request.Context(), body.Name)
	if err != nil {
		if errors.Is(err, ucversion.ErrVersionAlreadyExists) {
			apires.Fail(c, http.StatusBadRequest, "version already exists")
			return
		}
		panic(err)
	}

	apires.Created(c, v)
}
