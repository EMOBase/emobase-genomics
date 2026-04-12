package api

import (
	"net/http"

	"github.com/EMOBase/emobase-genomics/internal/pkg/apires"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

func Recovery(c *gin.Context, err any) {
	log.Ctx(c).Error().
		Interface("error", err).
		Msg("internal server error")

	apires.Fail(c, http.StatusInternalServerError, "internal server error")
}
