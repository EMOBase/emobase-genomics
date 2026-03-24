package main

import (
	"context"
	"os"

	"github.com/EMOBase/emobase-genomics/cmd/api"
	"github.com/EMOBase/emobase-genomics/cmd/worker"
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v3"
)

func main() {
	log.Info().Msg("hello, emobase genomics!")

	cmd := &cli.Command{
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "config",
				Aliases: []string{"c"},
				Value:   "internal/pkg/config/config.yaml",
				Usage:   "Configuration file",
			},
		},
		Commands: []*cli.Command{
			{
				Name:   "api",
				Usage:  "Run API server",
				Action: api.Action,
			},
			{
				Name:   "worker",
				Usage:  "Start a worker instance to process background jobs",
				Action: worker.Action,
			},
		},
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		log.Fatal().Err(err).Msg("application crashed")
	}

	log.Info().Msg("goodbye, emobase genomics!")
}
