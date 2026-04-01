package worker

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	configs "github.com/EMOBase/emobase-genomics/internal/pkg/config"
	"github.com/EMOBase/emobase-genomics/internal/pkg/database"
	repojob "github.com/EMOBase/emobase-genomics/internal/pkg/repository/job"
	ucworker "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/worker"
	"github.com/EMOBase/emobase-genomics/internal/pkg/usecase/worker/handlers"
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
	defer db.Close()

	jobHandlers := map[string]ucworker.Handler{
		ucworker.JobTypeGenomicFNA:   &handlers.GenomicFNAHandler{},
		ucworker.JobTypeGenomicGFF:   &handlers.GenomicGFFHandler{},
		ucworker.JobTypeRNAFNA:       &handlers.RNAFNAHandler{},
		ucworker.JobTypeCDSFNA:       &handlers.CDSFNAHandler{},
		ucworker.JobTypeProteinFAA:   &handlers.ProteinFAAHandler{},
		ucworker.JobTypeOrthologyTSV: &handlers.OrthologyTSVHandler{},
	}

	w := ucworker.New(
		repojob.NewMySQLRepository(db),
		jobHandlers,
		config.Jobs.PollInterval,
		config.Jobs.StuckInterval,
		config.Jobs.StuckTimeout,
	)

	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()

	log.Info().Msg("worker started")
	if err := w.Run(ctx); err != nil {
		return err
	}

	log.Info().Msg("worker stopped")
	return nil
}
