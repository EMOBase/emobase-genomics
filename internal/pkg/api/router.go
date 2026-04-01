package api

import (
	"net/http"

	"github.com/EMOBase/emobase-genomics/internal/pkg/api/handler"
	"github.com/EMOBase/emobase-genomics/internal/pkg/api/middleware"
	"github.com/EMOBase/emobase-genomics/internal/pkg/usecase/upload"
	ucversion "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/version"
	"github.com/gin-contrib/requestid"
	"github.com/gin-gonic/gin"
)

var skipLogPaths = map[string]struct{}{
	"/health":  {},
	"/uploads": {},
}

func NewRouter(uploadUC *upload.UseCase, versionUC *ucversion.UseCase) *gin.Engine {
	router := gin.New()
	router.Use(
		requestid.New(),
		gin.CustomRecovery(Recovery),
		middleware.NewRequestResponseLogger(skipLogPaths),
		middleware.NewCORSMiddleware(),
	)

	registerRoutes(router, uploadUC, versionUC)

	return router
}

func registerRoutes(router *gin.Engine, uploadUC *upload.UseCase, versionUC *ucversion.UseCase) {
	router.GET("/health", func(c *gin.Context) {
		c.AbortWithStatusJSON(http.StatusOK, "OK")
	})

	tusHandler := http.StripPrefix("/uploads", uploadUC.Handler)
	uploadHandler := func(c *gin.Context) {
		tusHandler.ServeHTTP(c.Writer, c.Request)
		c.Abort()
	}

	authenticated := router.Group("/", middleware.RequireAdmin())
	{
		authenticated.POST("/uploads", uploadHandler)
		authenticated.Any("/uploads/*any", uploadHandler)

		versionHandler := handler.NewVersionHandler(versionUC)
		authenticated.POST("/versions", versionHandler.Create)
	}
}
