package middleware

import (
	"net/http"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func NewCORSMiddleware() gin.HandlerFunc {
	corsConfig := cors.DefaultConfig()
	corsConfig.AddAllowMethods(http.MethodOptions)
	corsConfig.AllowAllOrigins = true

	corsConfig.AddAllowHeaders("Authorization")
	corsConfig.AddAllowHeaders("X-Request-ID")
	corsConfig.AddAllowHeaders("X-Client-ID")
	corsConfig.AddAllowHeaders("Accept-Version")

	return cors.New(corsConfig)
}
