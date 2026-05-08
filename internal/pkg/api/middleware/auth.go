package middleware

import (
	"net/http"
	"strings"

	"github.com/EMOBase/emobase-genomics/internal/pkg/apires"
	"github.com/EMOBase/emobase-genomics/internal/pkg/auth"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

func RequireAdmin(validator *auth.Validator) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			apires.AbortFail(c, http.StatusForbidden, "permission denied")
			return
		}

		email, err := validator.Validate(c.Request.Context(), authHeader)
		if err != nil {
			log.Ctx(c.Request.Context()).Error().Err(err).Msg("access token validation failed")
			apires.AbortFail(c, http.StatusForbidden, "permission denied")
			return
		}

		ctx := auth.WithUsername(c.Request.Context(), email)
		c.Request = c.Request.WithContext(ctx)

		c.Next()
	}
}
