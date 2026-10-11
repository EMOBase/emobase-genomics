package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/EMOBase/emobase-genomics/internal/pkg/apires"
	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	ucassemblyversion "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/assemblyversion"
	ucversion "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/version"
	"github.com/gin-gonic/gin"
)

type assemblyVersionUseCase interface {
	CreateAssemblyVersion(ctx context.Context, versionName, name, species string) (*entity.AssemblyVersion, error)
	ListAssemblyVersions(ctx context.Context, versionName string) ([]ucassemblyversion.AssemblyVersionItem, error)
	GetAssemblyVersionDetail(ctx context.Context, versionName string, assemblyVersionID uint64) (*ucversion.AssemblyVersionDetail, error)
	DeleteAssemblyVersion(ctx context.Context, versionName string, assemblyVersionID uint64) error
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

// assemblyVersionIDParam reads the `:id` path parameter, replying 400 itself
// when it is not an integer.
func assemblyVersionIDParam(c *gin.Context) (uint64, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		apires.Fail(c, http.StatusBadRequest, "assembly version id must be an integer")
		return 0, false
	}
	return id, true
}

func (h *AssemblyVersionHandler) Detail(c *gin.Context) {
	versionName := c.Param("name")
	id, ok := assemblyVersionIDParam(c)
	if !ok {
		return
	}

	detail, err := h.uc.GetAssemblyVersionDetail(c.Request.Context(), versionName, id)
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
	id, ok := assemblyVersionIDParam(c)
	if !ok {
		return
	}

	err := h.uc.DeleteAssemblyVersion(c.Request.Context(), versionName, id)
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
