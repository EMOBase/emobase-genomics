package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/EMOBase/emobase-genomics/internal/pkg/apires"
	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	ucassemblyversion "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/assemblyversion"
	ucversion "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/version"
	"github.com/gin-gonic/gin"
)

type assemblyVersionUseCase interface {
	CreateAssemblyVersion(ctx context.Context, versionName, name, species string) (*entity.AssemblyVersion, error)
	ListAssemblyVersions(ctx context.Context, versionName string) ([]ucassemblyversion.AssemblyVersionItem, error)
	GetAssemblyVersionDetail(ctx context.Context, versionName, species string) (*ucversion.AssemblyVersionDetail, error)
	DeleteAssemblyVersion(ctx context.Context, versionName, species string) error
}

type AssemblyVersionHandler struct {
	uc assemblyVersionUseCase
}

func NewAssemblyVersionHandler(uc assemblyVersionUseCase) *AssemblyVersionHandler {
	return &AssemblyVersionHandler{uc: uc}
}

func (h *AssemblyVersionHandler) Create(c *gin.Context) {
	versionName := c.Param("name")

	var body struct {
		Name    string `json:"name" binding:"required"`
		Species string `json:"species" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		apires.Fail(c, http.StatusBadRequest, err.Error())
		return
	}

	a, err := h.uc.CreateAssemblyVersion(c.Request.Context(), versionName, body.Name, body.Species)
	if err != nil {
		if errors.Is(err, ucassemblyversion.ErrVersionNotFound) {
			apires.Fail(c, http.StatusNotFound, "version not found")
			return
		}
		if errors.Is(err, ucassemblyversion.ErrAssemblyVersionAlreadyExists) {
			apires.Fail(c, http.StatusBadRequest, "assembly version already exists")
			return
		}
		panic(err)
	}

	apires.Created(c, a)
}

func (h *AssemblyVersionHandler) List(c *gin.Context) {
	versionName := c.Param("name")

	items, err := h.uc.ListAssemblyVersions(c.Request.Context(), versionName)
	if err != nil {
		if errors.Is(err, ucassemblyversion.ErrVersionNotFound) {
			apires.Fail(c, http.StatusNotFound, "version not found")
			return
		}
		panic(err)
	}

	apires.OK(c, items)
}

func (h *AssemblyVersionHandler) Detail(c *gin.Context) {
	versionName := c.Param("name")
	species := c.Param("species")

	detail, err := h.uc.GetAssemblyVersionDetail(c.Request.Context(), versionName, species)
	if err != nil {
		if errors.Is(err, ucassemblyversion.ErrVersionNotFound) {
			apires.Fail(c, http.StatusNotFound, "version not found")
			return
		}
		if errors.Is(err, ucassemblyversion.ErrAssemblyVersionNotFound) {
			apires.Fail(c, http.StatusNotFound, "assembly version not found")
			return
		}
		panic(err)
	}

	apires.OK(c, detail)
}

func (h *AssemblyVersionHandler) Delete(c *gin.Context) {
	versionName := c.Param("name")
	species := c.Param("species")

	err := h.uc.DeleteAssemblyVersion(c.Request.Context(), versionName, species)
	if err != nil {
		if errors.Is(err, ucassemblyversion.ErrVersionNotFound) {
			apires.Fail(c, http.StatusNotFound, "version not found")
			return
		}
		if errors.Is(err, ucassemblyversion.ErrAssemblyVersionNotFound) {
			apires.Fail(c, http.StatusNotFound, "assembly version not found")
			return
		}
		if errors.Is(err, ucassemblyversion.ErrAssemblyVersionHasActiveJobs) {
			apires.Fail(c, http.StatusUnprocessableEntity, err.Error())
			return
		}
		panic(err)
	}

	apires.NoContent(c)
}
