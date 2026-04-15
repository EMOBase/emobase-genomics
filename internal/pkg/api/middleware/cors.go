package middleware

import (
	"net/http"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func NewCORSMiddleware() gin.HandlerFunc {
	corsConfig := cors.DefaultConfig()
	corsConfig.AddAllowMethods(
		http.MethodOptions,
		http.MethodHead,
		http.MethodPatch,
		http.MethodPost,
	)
	corsConfig.AllowAllOrigins = true

	corsConfig.AddAllowHeaders("Authorization")
	corsConfig.AddAllowHeaders("X-Request-ID")
	corsConfig.AddAllowHeaders("X-Client-ID")
	corsConfig.AddAllowHeaders("Accept-Version")
	corsConfig.AddAllowHeaders("Tus-Resumable")
	corsConfig.AddAllowHeaders("Upload-Length")
	corsConfig.AddAllowHeaders("Upload-Metadata")
	corsConfig.AddAllowHeaders("Upload-Offset")
	corsConfig.AddAllowHeaders("Tus-Version")
	corsConfig.AddAllowHeaders("Tus-Extension")
	corsConfig.AddAllowHeaders("Tus-Max-Size")
	corsConfig.AddAllowHeaders("Content-Type")

	corsConfig.AddExposeHeaders("Location")
	corsConfig.AddExposeHeaders("Tus-Resumable")
	corsConfig.AddExposeHeaders("Upload-Offset")
	corsConfig.AddExposeHeaders("Upload-Length")
	corsConfig.AddExposeHeaders("Upload-Metadata")
	corsConfig.AddExposeHeaders("Tus-Version")
	corsConfig.AddExposeHeaders("Tus-Extension")
	corsConfig.AddExposeHeaders("Tus-Max-Size")
	corsConfig.AddExposeHeaders("X-Job-IDs")

	return cors.New(corsConfig)
}
