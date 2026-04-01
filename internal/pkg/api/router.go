package api

import (
	"net/http"

	"github.com/EMOBase/emobase-genomics/internal/pkg/api/middleware"
	"github.com/EMOBase/emobase-genomics/internal/pkg/usecase/upload"
	"github.com/gin-contrib/requestid"
	"github.com/gin-gonic/gin"
)

var skipLogPaths = map[string]struct{}{
	"/health":  {},
	"/uploads": {},
}

func NewRouter(uploadUC *upload.UseCase) *gin.Engine {
	router := gin.New()
	router.Use(
		requestid.New(),
		gin.CustomRecovery(Recovery),
		middleware.NewRequestResponseLogger(skipLogPaths),
		middleware.NewCORSMiddleware(),
	)

	registerRoutes(router, uploadUC)

	return router
}

func registerRoutes(router *gin.Engine, uploadUC *upload.UseCase) {
	router.GET("/health", func(c *gin.Context) {
		c.AbortWithStatusJSON(http.StatusOK, "OK")
	})

	tusHandler := http.StripPrefix("/uploads", uploadUC.Handler)
	uploadHandler := func(c *gin.Context) {
		tusHandler.ServeHTTP(c.Writer, c.Request)
		c.Abort()
	}

	router.POST("/uploads", uploadHandler)
	router.Any("/uploads/*any", uploadHandler)
}
