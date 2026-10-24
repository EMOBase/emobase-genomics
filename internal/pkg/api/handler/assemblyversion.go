package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/EMOBase/emobase-genomics/internal/pkg/apires"
	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	ucassemblyversion "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/assemblyversion"
	ucversion "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/version"
	"github.com/gin-gonic/gin"
)

type assemblyVersionUseCase interface {
	CreateAssemblyVersion(ctx context.Context, versionName, name, speciesName, infoURL string) (*entity.AssemblyVersion, error)
	UpdateAssemblyVersion(ctx context.Context, versionName string, assemblyVersionID uint64, upd ucassemblyversion.AssemblyVersionUpdate) (*entity.AssemblyVersion, error)
	SetAssemblyVersionImage(ctx context.Context, versionName string, assemblyVersionID uint64, image io.ReadSeeker, size int64) (*entity.AssemblyVersion, error)
	DeleteAssemblyVersionImage(ctx context.Context, versionName string, assemblyVersionID uint64) error
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
		Name        string `json:"name" binding:"required"`
		SpeciesName string `json:"speciesName" binding:"required"`
		InfoURL     string `json:"infoUrl"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		apires.Fail(c, http.StatusBadRequest, err.Error())
		return
	}

	a, err := h.uc.CreateAssemblyVersion(c.Request.Context(), versionName, body.Name, body.SpeciesName, body.InfoURL)
	if err != nil {
		if errors.Is(err, ucassemblyversion.ErrVersionNotFound) {
			apires.Fail(c, http.StatusNotFound, "version not found")
			return
		}
		if errors.Is(err, ucassemblyversion.ErrInvalidInfoURL) || errors.Is(err, ucassemblyversion.ErrInvalidSpeciesName) {
			apires.Fail(c, http.StatusBadRequest, err.Error())
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

// failAssemblyNotFound replies 404 when err is a version or assembly lookup
// miss, and reports whether it did.
func failAssemblyNotFound(c *gin.Context, err error) bool {
	if errors.Is(err, ucassemblyversion.ErrVersionNotFound) {
		apires.Fail(c, http.StatusNotFound, "version not found")
		return true
	}
	if errors.Is(err, ucassemblyversion.ErrAssemblyVersionNotFound) {
		apires.Fail(c, http.StatusNotFound, "assembly version not found")
		return true
	}
	return false
}

func (h *AssemblyVersionHandler) Update(c *gin.Context) {
	versionName := c.Param("name")
	id, ok := assemblyVersionIDParam(c)
	if !ok {
		return
	}

	var body struct {
		Name        *string `json:"name"`
		SpeciesName *string `json:"speciesName"`
		SpeciesCode *string `json:"speciesCode"`
		InfoURL     *string `json:"infoUrl"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		apires.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	// Rejected rather than ignored, so a client never believes it changed.
	if body.SpeciesName != nil || body.SpeciesCode != nil {
		apires.Fail(c, http.StatusBadRequest, "species cannot be changed; delete and recreate the assembly instead")
		return
	}

	a, err := h.uc.UpdateAssemblyVersion(c.Request.Context(), versionName, id, ucassemblyversion.AssemblyVersionUpdate{
		Name:    body.Name,
		InfoURL: body.InfoURL,
	})
	if err != nil {
		if failAssemblyNotFound(c, err) {
			return
		}
		if errors.Is(err, ucassemblyversion.ErrEmptyName) || errors.Is(err, ucassemblyversion.ErrInvalidInfoURL) {
			apires.Fail(c, http.StatusBadRequest, err.Error())
			return
		}
		panic(err)
	}

	apires.OK(c, a)
}

func (h *AssemblyVersionHandler) SetImage(c *gin.Context) {
	versionName := c.Param("name")
	id, ok := assemblyVersionIDParam(c)
	if !ok {
		return
	}

	// Cut off the request a little above the image limit (room for multipart
	// overhead) instead of buffering an oversized body to disk.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, ucassemblyversion.MaxImageSize+1<<20)
	fh, err := c.FormFile("image")
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			apires.Fail(c, http.StatusRequestEntityTooLarge, ucassemblyversion.ErrImageTooLarge.Error())
			return
		}
		apires.Fail(c, http.StatusBadRequest, `multipart form field "image" is required`)
		return
	}
	f, err := fh.Open()
	if err != nil {
		panic(err)
	}
	defer func() { _ = f.Close() }()

	a, err := h.uc.SetAssemblyVersionImage(c.Request.Context(), versionName, id, f, fh.Size)
	if err != nil {
		if failAssemblyNotFound(c, err) {
			return
		}
		if errors.Is(err, ucassemblyversion.ErrImageTooLarge) {
			apires.Fail(c, http.StatusRequestEntityTooLarge, err.Error())
			return
		}
		if errors.Is(err, ucassemblyversion.ErrUnsupportedImageType) {
			apires.Fail(c, http.StatusBadRequest, err.Error())
			return
		}
		panic(err)
	}

	apires.OK(c, a)
}

func (h *AssemblyVersionHandler) DeleteImage(c *gin.Context) {
	versionName := c.Param("name")
	id, ok := assemblyVersionIDParam(c)
	if !ok {
		return
	}

	if err := h.uc.DeleteAssemblyVersionImage(c.Request.Context(), versionName, id); err != nil {
		if failAssemblyNotFound(c, err) {
			return
		}
		panic(err)
	}

	apires.NoContent(c)
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
