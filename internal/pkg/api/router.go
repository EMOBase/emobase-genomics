package api

import (
	"net/http"

	"github.com/EMOBase/emobase-genomics/internal/pkg/api/handler"
	"github.com/EMOBase/emobase-genomics/internal/pkg/api/middleware"
	"github.com/EMOBase/emobase-genomics/internal/pkg/apires"
	ucjob "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/job"
	"github.com/EMOBase/emobase-genomics/internal/pkg/usecase/upload"
	ucversion "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/version"
	"github.com/gin-contrib/requestid"
	"github.com/gin-gonic/gin"
)

var skipLogPaths = map[string]struct{}{
	"/health":  {},
	"/uploads": {},
}

func NewRouter(uploadUC *upload.UseCase, versionUC *ucversion.UseCase, jobUC *ucjob.UseCase) *gin.Engine {
	router := gin.New()
	router.Use(
		requestid.New(),
		gin.CustomRecovery(Recovery),
		middleware.NewRequestResponseLogger(skipLogPaths),
		middleware.NewCORSMiddleware(),
	)

	registerRoutes(router, uploadUC, versionUC, jobUC)

	return router
}

func registerRoutes(router *gin.Engine, uploadUC *upload.UseCase, versionUC *ucversion.UseCase, jobUC *ucjob.UseCase) {
	router.GET("/health", func(c *gin.Context) {
		apires.OK(c, "OK")
		c.Abort()
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
		authenticated.GET("/versions", versionHandler.List)
		authenticated.POST("/versions", versionHandler.Create)
		authenticated.POST("/versions/default", versionHandler.SetDefault)

		jobHandler := handler.NewJobHandler(jobUC)
		authenticated.GET("/jobs", jobHandler.ListByVersion)
	}
}
