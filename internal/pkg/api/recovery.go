package api

import (
	"net/http"

	"github.com/gin-contrib/requestid"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

func Recovery(c *gin.Context, err any) {
	log.Ctx(c).Error().
		Interface("error", err).
		Msg("internal server error")

	c.JSON(
		http.StatusInternalServerError,
		struct {
			Message   string `json:"message"`
			RequestID string `json:"requestID"`
		}{
			Message:   "internal server error",
			RequestID: requestid.Get(c),
		},
	)
}
