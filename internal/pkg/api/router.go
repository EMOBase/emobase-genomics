package api

import (
	"net/http"

	"github.com/EMOBase/emobase-genomics/internal/pkg/api/handler"
	"github.com/EMOBase/emobase-genomics/internal/pkg/api/middleware"
	"github.com/EMOBase/emobase-genomics/internal/pkg/apires"
	"github.com/EMOBase/emobase-genomics/internal/pkg/auth"
	ucjob "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/job"
	ucsearch "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/search"
	"github.com/EMOBase/emobase-genomics/internal/pkg/usecase/upload"
	ucversion "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/version"
	"github.com/gin-contrib/requestid"
	"github.com/gin-gonic/gin"
)

var skipLogPaths = map[string]struct{}{
	"/health":  {},
	"/uploads": {},
}

func NewRouter(uploadUC *upload.UseCase, versionUC *ucversion.UseCase, jobUC *ucjob.UseCase, searchUC *ucsearch.UseCase, validator *auth.Validator) *gin.Engine {
	router := gin.New()
	router.Use(
		requestid.New(),
		gin.CustomRecovery(Recovery),
		middleware.NewRequestResponseLogger(skipLogPaths),
		middleware.NewCORSMiddleware(),
	)

	registerRoutes(router, uploadUC, versionUC, jobUC, searchUC, validator)

	return router
}

func registerRoutes(router *gin.Engine, uploadUC *upload.UseCase, versionUC *ucversion.UseCase, jobUC *ucjob.UseCase, searchUC *ucsearch.UseCase, validator *auth.Validator) {
	router.GET("/health", func(c *gin.Context) {
		apires.OK(c, "OK")
		c.Abort()
	})

	searchHandler := handler.NewSearchHandler(searchUC)
	router.GET("/search", searchHandler.Search)
	router.GET("/search/_suggest", searchHandler.Suggest)

	orthologyHandler := handler.NewOrthologyHandler(searchUC)
	router.GET("/orthology/:species", orthologyHandler.BySpecies)

	genesHandler := handler.NewGenesHandler(searchUC)
	router.GET("/genes/:species", genesHandler.BySpecies)

	tusHandler := http.StripPrefix("/uploads", uploadUC.Handler)
	uploadHandler := func(c *gin.Context) {
		tusHandler.ServeHTTP(c.Writer, c.Request)
		c.Abort()
	}

	authenticated := router.Group("/", middleware.RequireAdmin(validator))
	{
		authenticated.POST("/uploads", uploadHandler)
		authenticated.Any("/uploads/*any", uploadHandler)

		versionHandler := handler.NewVersionHandler(versionUC)
		authenticated.GET("/versions", versionHandler.List)
		authenticated.GET("/versions/:name/detail", versionHandler.Detail)
		authenticated.POST("/versions", versionHandler.Create)
		authenticated.DELETE("/versions/:name", versionHandler.Delete)
		authenticated.POST("/versions/:name/release", versionHandler.Release)

		jobHandler := handler.NewJobHandler(jobUC)
		authenticated.GET("/jobs", jobHandler.ListByVersion)

		uploadFileHandler := handler.NewUploadFileHandler(uploadUC)
		authenticated.GET("/upload-files", uploadFileHandler.List)
		authenticated.DELETE("/upload-files/:id", uploadFileHandler.Delete)
	}
}
