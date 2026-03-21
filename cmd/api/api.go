package api

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/EMOBase/emobase-genomics/internal/pkg/api"
	"github.com/EMOBase/emobase-genomics/internal/pkg/api/middleware"
	configs "github.com/EMOBase/emobase-genomics/internal/pkg/config"
	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/requestid"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v3"
)

const healthAPIPath = "/health"

var skipLogPaths = map[string]struct{}{
	healthAPIPath: {},
}

func Action(ctx context.Context, cmd *cli.Command) error {
	configFile := cmd.String("config")
	config, err := configs.LoadConfig(configFile)
	if err != nil {
		return err
	}

	// Init TUS handler
	tusHandler, err := NewTUSHandler()
	if err != nil {
		return err
	}

	// Init Gin server
	gin.SetMode(config.HTTP.Mode)

	router := gin.New()
	router.Use(
		requestid.New(),
		gin.CustomRecovery(api.Recovery),
		middleware.NewRequestResponseLogger(skipLogPaths),
		corsMiddleware(),
	)

	router.GET(healthAPIPath, func(c *gin.Context) {
		c.AbortWithStatusJSON(http.StatusOK, "OK")
	})

	api := router.Group("/api")

	// Use ANY to support all TUS methods (PATCH, HEAD, OPTIONS, etc.)
	api.Any("/uploads", func(c *gin.Context) {
		tusHandler.ServeHTTP(c.Writer, c.Request)
		c.Abort()
	})

	httpServer := &http.Server{
		Addr:    config.HTTP.BindAddress,
		Handler: router,
	}

	return listenAndServe(httpServer)
}

func listenAndServe(httpServer *http.Server) error {
	stop := make(chan os.Signal, 1)
	errCh := make(chan error)
	signal.Notify(stop, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	go func() {
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

func corsMiddleware() gin.HandlerFunc {
	corsConfig := cors.DefaultConfig()
	corsConfig.AddAllowMethods(http.MethodOptions)
	corsConfig.AllowAllOrigins = true

	corsConfig.AddAllowHeaders("Authorization")
	corsConfig.AddAllowHeaders("X-Request-ID")
	corsConfig.AddAllowHeaders("X-Client-ID")
	corsConfig.AddAllowHeaders("Accept-Version")

	return cors.New(corsConfig)
}
