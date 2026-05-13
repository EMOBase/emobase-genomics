package api

import (
	"net/http"

	"github.com/EMOBase/emobase-genomics/internal/pkg/api/handler"
	"github.com/EMOBase/emobase-genomics/internal/pkg/api/middleware"
	"github.com/EMOBase/emobase-genomics/internal/pkg/apires"
	"github.com/EMOBase/emobase-genomics/internal/pkg/auth"
	ucjob "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/job"
	"github.com/EMOBase/emobase-genomics/internal/pkg/usecase/upload"
	ucversion "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/version"
	"github.com/gin-contrib/requestid"
	"github.com/gin-gonic/gin"
)

var skipLogPaths = map[string]struct{}{
	"/health":     {},
	"/uploads":    {},
	"/v1/uploads": {},
}

func NewRouter(uploadUC *upload.UseCase, versionUC *ucversion.UseCase, jobUC *ucjob.UseCase, validator *auth.Validator) *gin.Engine {
	router := gin.New()
	router.Use(
		requestid.New(),
		gin.CustomRecovery(Recovery),
		middleware.NewRequestResponseLogger(skipLogPaths),
		middleware.NewCORSMiddleware(),
	)

	registerRoutes(router, uploadUC, versionUC, jobUC, validator)

	return router
}

func registerRoutes(router *gin.Engine, uploadUC *upload.UseCase, versionUC *ucversion.UseCase, jobUC *ucjob.UseCase, validator *auth.Validator) {
	router.GET("/health", func(c *gin.Context) {
		apires.OK(c, "OK")
		c.Abort()
	})

	makeUploadHandler := func(prefix string) gin.HandlerFunc {
		h := http.StripPrefix(prefix+"/uploads", uploadUC.Handler)
		return func(c *gin.Context) {
			h.ServeHTTP(c.Writer, c.Request)
			c.Abort()
		}
	}

	registerAPIRoutes(router.Group("/", middleware.RequireAdmin(validator)), makeUploadHandler(""), versionUC, jobUC, uploadUC)
	registerAPIRoutes(router.Group("/v1", middleware.RequireAdmin(validator)), makeUploadHandler("/v1"), versionUC, jobUC, uploadUC)
}

func registerAPIRoutes(rg *gin.RouterGroup, uploadHandler gin.HandlerFunc, versionUC *ucversion.UseCase, jobUC *ucjob.UseCase, uploadUC *upload.UseCase) {
	rg.POST("/uploads", uploadHandler)
	rg.Any("/uploads/*any", uploadHandler)

	versionHandler := handler.NewVersionHandler(versionUC)
	rg.GET("/versions", versionHandler.List)
	rg.GET("/versions/:name/detail", versionHandler.Detail)
	rg.POST("/versions", versionHandler.Create)
	rg.DELETE("/versions/:name", versionHandler.Delete)
	rg.POST("/versions/:name/release", versionHandler.Release)

	jobHandler := handler.NewJobHandler(jobUC)
	rg.GET("/jobs", jobHandler.ListByVersion)

	uploadFileHandler := handler.NewUploadFileHandler(uploadUC)
	rg.GET("/upload-files", uploadFileHandler.List)
	rg.DELETE("/upload-files/:id", uploadFileHandler.Delete)
}
