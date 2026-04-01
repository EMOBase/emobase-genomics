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
	configs "github.com/EMOBase/emobase-genomics/internal/pkg/config"
	"github.com/EMOBase/emobase-genomics/internal/pkg/usecase/upload"
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

	uploadUC, err := upload.New("./public/uploads")
	if err != nil {
		return err
	}

	gin.SetMode(config.HTTP.Mode)

	router := pkgapi.NewRouter(uploadUC)

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
