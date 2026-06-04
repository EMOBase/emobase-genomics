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
	GetGenesBySpecies(ctx context.Context, species, ids, versionName string) ([]ucsearch.GeneDetail, error)
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
	if ids == "" {
		apires.Fail(c, http.StatusBadRequest, "ids parameter is required")
		return
	}

	results, err := h.uc.GetGenesBySpecies(c.Request.Context(), species, ids, c.Query("version"))
	if err != nil {
		if errors.Is(err, ucsearch.ErrVersionNotFound) {
			apires.Fail(c, http.StatusNotFound, "version not found")
			return
		}
		if errors.Is(err, ucsearch.ErrNoDefaultVersion) {
			apires.Fail(c, http.StatusUnprocessableEntity, "no default version configured")
			return
		}
		panic(err)
	}

	apires.OK(c, results)
}
