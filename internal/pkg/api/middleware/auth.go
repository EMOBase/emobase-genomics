package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"message": "permission denied"})
			return
		}
		c.Next()
	}
}
