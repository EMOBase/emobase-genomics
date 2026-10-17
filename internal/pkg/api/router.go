package api

import (
	"net/http"

	"github.com/EMOBase/emobase-genomics/internal/pkg/api/handler"
	"github.com/EMOBase/emobase-genomics/internal/pkg/api/middleware"
	"github.com/EMOBase/emobase-genomics/internal/pkg/apires"
	"github.com/EMOBase/emobase-genomics/internal/pkg/auth"
	ucassemblyversion "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/assemblyversion"
	ucjob "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/job"
	ucsearch "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/search"
	"github.com/EMOBase/emobase-genomics/internal/pkg/usecase/upload"
	ucversion "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/version"
	"github.com/gin-contrib/requestid"
	"github.com/gin-gonic/gin"
)

var skipLogPaths = map[string]struct{}{
	"/health":            {},
	"/uploads":           {},
	"/docs":              {},
	"/docs/openapi.yaml": {},
}

func NewRouter(uploadUC *upload.UseCase, versionUC *ucversion.UseCase, assemblyVersionUC *ucassemblyversion.UseCase, jobUC *ucjob.UseCase, searchUC *ucsearch.UseCase, validator *auth.Validator) *gin.Engine {
	router := gin.New()
	router.Use(
		requestid.New(),
		gin.CustomRecovery(Recovery),
		middleware.NewRequestResponseLogger(skipLogPaths),
		middleware.NewCORSMiddleware(),
	)

	registerRoutes(router, uploadUC, versionUC, assemblyVersionUC, jobUC, searchUC, validator)

	return router
}

func registerRoutes(router *gin.Engine, uploadUC *upload.UseCase, versionUC *ucversion.UseCase, assemblyVersionUC *ucassemblyversion.UseCase, jobUC *ucjob.UseCase, searchUC *ucsearch.UseCase, validator *auth.Validator) {
	router.GET("/health", func(c *gin.Context) {
		apires.OK(c, "OK")
		c.Abort()
	})

	router.GET("/docs", handler.ServeAPIDocs)
	router.GET("/docs/openapi.yaml", handler.ServeOpenAPISpec)

	searchHandler := handler.NewSearchHandler(searchUC)
	router.GET("/search", searchHandler.Search)
	router.GET("/search/_suggest", searchHandler.Suggest)

	orthologyHandler := handler.NewOrthologyHandler(searchUC)
	router.GET("/orthology/:species", orthologyHandler.BySpecies)

	genesHandler := handler.NewGenesHandler(searchUC)
	router.GET("/genes/:species", genesHandler.BySpecies)

	silencingSeqsHandler := handler.NewSilencingSeqsHandler(searchUC)
	router.GET("/silencingseqs", silencingSeqsHandler.Get)

	versionHandler := handler.NewVersionHandler(versionUC)
	router.GET("/public/versions", versionHandler.ListPublic)
	router.GET("/versions", versionHandler.List)
	router.GET("/versions/:name/detail", versionHandler.Detail)
	router.GET("/versions/:name/orthologies", versionHandler.Orthologies)

	assemblyVersionHandler := handler.NewAssemblyVersionHandler(assemblyVersionUC)
	router.GET("/versions/:name/assemblies", assemblyVersionHandler.List)
	router.GET("/versions/:name/assemblies/:id", assemblyVersionHandler.Detail)

	tusHandler := http.StripPrefix("/uploads", uploadUC.Handler)
	uploadHandler := func(c *gin.Context) {
		tusHandler.ServeHTTP(c.Writer, c.Request)
		c.Abort()
	}

	authenticated := router.Group("/", middleware.RequireAdmin(validator))
	{
		authenticated.POST("/uploads", uploadHandler)
		authenticated.Any("/uploads/*any", uploadHandler)

		authenticated.POST("/versions", versionHandler.Create)
		authenticated.DELETE("/versions/:name", versionHandler.Delete)
		authenticated.POST("/versions/:name/release", versionHandler.Release)

		authenticated.POST("/versions/:name/assemblies", assemblyVersionHandler.Create)
		authenticated.DELETE("/versions/:name/assemblies/:id", assemblyVersionHandler.Delete)

		jobHandler := handler.NewJobHandler(jobUC)
		authenticated.GET("/jobs", jobHandler.ListByVersion)

		uploadFileHandler := handler.NewUploadFileHandler(uploadUC)
		authenticated.GET("/upload-files", uploadFileHandler.List)
		authenticated.GET("/upload-files/templates/:fileType", uploadFileHandler.ManifestTemplate)
		authenticated.DELETE("/upload-files/:id", uploadFileHandler.Delete)
	}
}
