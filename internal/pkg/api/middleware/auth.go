package middleware

import (
	"net/http"
	"strings"

	"github.com/EMOBase/emobase-genomics/internal/pkg/auth"
	"github.com/gin-gonic/gin"
)

func RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"message": "permission denied"})
			return
		}

		username, err := auth.DecodeUsername(authHeader)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"message": "permission denied"})
			return
		}

		ctx := auth.WithUsername(c.Request.Context(), username)
		c.Request = c.Request.WithContext(ctx)

		c.Next()
	}
}
