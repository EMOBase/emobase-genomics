package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/EMOBase/emobase-genomics/internal/pkg/apires"
	ucsearch "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/search"
	"github.com/gin-gonic/gin"
)

type genesUseCase interface {
	GetGenesBySpecies(ctx context.Context, species, ids, symbol, fullname, annotationID, versionName string) ([]ucsearch.GeneDetail, error)
}

type GenesHandler struct {
	uc genesUseCase
}

func NewGenesHandler(uc genesUseCase) *GenesHandler {
	return &GenesHandler{uc: uc}
}

func (h *GenesHandler) BySpecies(c *gin.Context) {
	species := c.Param("species")
	ids := c.Query("ids")
	symbol := c.Query("symbol")
	fullname := c.Query("fullname")
	annotationID := c.Query("annotationId")

	n := countNonEmpty(ids, symbol, fullname, annotationID)
	if n == 0 {
		apires.Fail(c, http.StatusBadRequest, "one of ids, symbol, fullname, annotationId is required")
		return
	}
	if n > 1 {
		apires.Fail(c, http.StatusBadRequest, "only one of ids, symbol, fullname, annotationId is allowed")
		return
	}

	results, err := h.uc.GetGenesBySpecies(c.Request.Context(), species, ids, symbol, fullname, annotationID, c.Query("version"))
	if err != nil {
		if errors.Is(err, ucsearch.ErrVersionNotFound) {
			apires.Fail(c, http.StatusNotFound, "version not found")
			return
		}
		if errors.Is(err, ucsearch.ErrNoDefaultVersion) {
			apires.OK(c, []ucsearch.GeneDetail{})
			return
		}
		panic(err)
	}

	apires.OK(c, results)
}

func countNonEmpty(ss ...string) int {
	n := 0
	for _, s := range ss {
		if s != "" {
			n++
		}
	}
	return n
}
