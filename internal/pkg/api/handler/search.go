package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/EMOBase/emobase-genomics/internal/pkg/apires"
	ucsearch "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/search"
	"github.com/gin-gonic/gin"
)

type searchUseCase interface {
	Search(ctx context.Context, query, versionName string) (*ucsearch.SearchResult, error)
}

type SearchHandler struct {
	uc searchUseCase
}

func NewSearchHandler(uc searchUseCase) *SearchHandler {
	return &SearchHandler{uc: uc}
}

func (h *SearchHandler) Search(c *gin.Context) {
	query := c.Query("query")
	if query == "" {
		apires.Fail(c, http.StatusBadRequest, "query parameter is required")
		return
	}

	result, err := h.uc.Search(c.Request.Context(), query, c.Query("version"))
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

	apires.OK(c, result)
}
