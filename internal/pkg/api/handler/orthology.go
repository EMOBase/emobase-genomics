package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/EMOBase/emobase-genomics/internal/pkg/apires"
	ucsearch "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/search"
	"github.com/gin-gonic/gin"
)

type orthologyUseCase interface {
	GetOrthologyBySpecies(ctx context.Context, species, genes, source, versionName string) ([]ucsearch.GeneOrthology, error)
}

type OrthologyHandler struct {
	uc orthologyUseCase
}

func NewOrthologyHandler(uc orthologyUseCase) *OrthologyHandler {
	return &OrthologyHandler{uc: uc}
}

func (h *OrthologyHandler) BySpecies(c *gin.Context) {
	species := c.Param("species")
	genes := c.Query("genes")
	if genes == "" {
		apires.Fail(c, http.StatusBadRequest, "genes parameter is required")
		return
	}
	source := c.DefaultQuery("source", "all")

	results, err := h.uc.GetOrthologyBySpecies(c.Request.Context(), species, genes, source, c.Query("version"))
	if err != nil {
		if errors.Is(err, ucsearch.ErrVersionNotFound) {
			apires.Fail(c, http.StatusNotFound, "version not found")
			return
		}
		if errors.Is(err, ucsearch.ErrNoDefaultVersion) {
			apires.OK(c, []ucsearch.GeneOrthology{})
			return
		}
		panic(err)
	}

	apires.OK(c, results)
}
