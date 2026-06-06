package api

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	pkgapi "github.com/EMOBase/emobase-genomics/internal/pkg/api"
	"github.com/EMOBase/emobase-genomics/internal/pkg/auth"
	configs "github.com/EMOBase/emobase-genomics/internal/pkg/config"
	"github.com/EMOBase/emobase-genomics/internal/pkg/database"
	repoappsettings "github.com/EMOBase/emobase-genomics/internal/pkg/repository/appsettings"
	"github.com/EMOBase/emobase-genomics/internal/pkg/repository/esindex"
	repogenomic "github.com/EMOBase/emobase-genomics/internal/pkg/repository/genomic"
	repojob "github.com/EMOBase/emobase-genomics/internal/pkg/repository/job"
	repoorthology "github.com/EMOBase/emobase-genomics/internal/pkg/repository/orthology"
	reposequence "github.com/EMOBase/emobase-genomics/internal/pkg/repository/sequence"
	reposynonym "github.com/EMOBase/emobase-genomics/internal/pkg/repository/synonym"
	repouploadfile "github.com/EMOBase/emobase-genomics/internal/pkg/repository/uploadfile"
	repoversion "github.com/EMOBase/emobase-genomics/internal/pkg/repository/version"
	ucjob "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/job"
	ucsearch "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/search"
	"github.com/EMOBase/emobase-genomics/internal/pkg/usecase/upload"
	ucversion "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/version"
	"github.com/EMOBase/emobase-genomics/internal/pkg/usecase/versionresolver"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v3"
)

func Action(ctx context.Context, cmd *cli.Command) error {
	configFile := cmd.String("config")
	config, err := configs.LoadConfig(configFile)
	if err != nil {
		return err
	}

	db, err := database.NewMySQL(config.MySQL)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	esClient, err := database.NewElasticsearch(config.Elasticsearch)
	if err != nil {
		return err
	}

	jobRepo := repojob.New(db)
	appSettingsRepo := repoappsettings.New(db)

	versionRepo := repoversion.New(db)
	uploadFileRepo := repouploadfile.New(db)
	versionUC := ucversion.New(versionRepo, appSettingsRepo, jobRepo, uploadFileRepo, esindex.New(esClient, config.Elasticsearch.IndexPrefix))

	jobUC := ucjob.New(jobRepo, versionRepo)

	batchSize := config.Elasticsearch.BulkBatchSize
	searchUC := ucsearch.New(
		reposynonym.New(esClient, batchSize),
		repoorthology.New(esClient, batchSize),
		reposequence.New(esClient, batchSize),
		repogenomic.New(esClient, batchSize),
		versionresolver.New(versionRepo, appSettingsRepo),
		config.Elasticsearch.IndexPrefix,
		config.MainSpecies,
	)

	uploadUC, err := upload.New(
		config.Uploads.Dir,
		config.Uploads.TUSBasePath,
		config.JBrowse2.GeneLinkBase,
		config.MainSpecies,
		config.Uploads.StaleAfter,
		versionRepo,
		jobRepo,
		uploadFileRepo,
	)
	if err != nil {
		return err
	}

	validator, err := auth.NewValidator(ctx, config.Keycloak.URL, config.Keycloak.Realm, config.Keycloak.Issuer, config.Keycloak.RequiredRole, config.Keycloak.DevBypassAuth)
	if err != nil {
		return err
	}

	gin.SetMode(config.HTTP.Mode)

	router := pkgapi.NewRouter(uploadUC, versionUC, jobUC, searchUC, validator)

	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", config.HTTP.Port),
		Handler: router,
	}

	return listenAndServe(httpServer)
}

func listenAndServe(httpServer *http.Server) error {
	stop := make(chan os.Signal, 1)
	errCh := make(chan error)
	signal.Notify(stop, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Info().
			Str("address", httpServer.Addr).
			Msg("starting api server")
		if err := httpServer.ListenAndServe(); err != nil {
			errCh <- err
		}
	}()

	for {
		select {
		case <-stop:
			shutdownCtx, cancelFn := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancelFn()

			if err := httpServer.Shutdown(shutdownCtx); err != nil {
				log.Ctx(shutdownCtx).Err(err).Msg("failed to stop server")
			}

			return nil
		case err := <-errCh:
			return err
		}
	}
}
