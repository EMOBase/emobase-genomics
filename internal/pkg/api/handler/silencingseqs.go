package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/EMOBase/emobase-genomics/internal/pkg/apires"
	ucsearch "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/search"
	"github.com/gin-gonic/gin"
)

type silencingSeqsUseCase interface {
	GetSilencingSeqs(ctx context.Context, species string, ids, geneIDs []string, versionName string) ([]ucsearch.SilencingSeq, error)
}

type SilencingSeqsHandler struct {
	uc silencingSeqsUseCase
}

func NewSilencingSeqsHandler(uc silencingSeqsUseCase) *SilencingSeqsHandler {
	return &SilencingSeqsHandler{uc: uc}
}

// Get serves GET /silencingseqs/:species and the deprecated GET /silencingseqs,
// whose empty species means the configured main species.
func (h *SilencingSeqsHandler) Get(c *gin.Context) {
	ids := splitCSV(c.Query("ids"))
	geneIDs := splitCSV(c.Query("geneIds"))

	if len(ids) > 0 && len(geneIDs) > 0 {
		apires.Fail(c, http.StatusBadRequest, `"ids" and "geneIds" must not be used together`)
		return
	}

	results, err := h.uc.GetSilencingSeqs(c.Request.Context(), c.Param("species"), ids, geneIDs, c.Query("version"))
	if err != nil {
		if errors.Is(err, ucsearch.ErrVersionNotFound) {
			apires.Fail(c, http.StatusNotFound, "version not found")
			return
		}
		if errors.Is(err, ucsearch.ErrNoDefaultVersion) {
			apires.OK(c, []ucsearch.SilencingSeq{})
			return
		}
		panic(err)
	}

	apires.OK(c, results)
}

func splitCSV(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}
