package worker

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	configs "github.com/EMOBase/emobase-genomics/internal/pkg/config"
	"github.com/EMOBase/emobase-genomics/internal/pkg/database"
	repogenomic "github.com/EMOBase/emobase-genomics/internal/pkg/repository/genomic"
	repojob "github.com/EMOBase/emobase-genomics/internal/pkg/repository/job"
	repoorthology "github.com/EMOBase/emobase-genomics/internal/pkg/repository/orthology"
	reposequence "github.com/EMOBase/emobase-genomics/internal/pkg/repository/sequence"
	reposynonym "github.com/EMOBase/emobase-genomics/internal/pkg/repository/synonym"
	repoversion "github.com/EMOBase/emobase-genomics/internal/pkg/repository/version"
	ucgenomic "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/genomic"
	ucorthology "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/orthology"
	ucsequence "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/sequence"
	ucsynonym "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/synonym"
	synonymparser "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/synonym/parser"
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

	esClient, err := database.NewElasticsearch(config.Elasticsearch)
	if err != nil {
		return err
	}

	versionRepo := repoversion.New(db)

	genomicRepo := repogenomic.New(esClient)
	genomicUC := ucgenomic.New(genomicRepo, config.MainSpecies)

	sequenceRepo := reposequence.New(esClient)
	sequenceUC := ucsequence.New(sequenceRepo, config.MainSpecies)

	orthologyRepo := repoorthology.New(esClient)
	orthologyUC := ucorthology.New(orthologyRepo)

	synonymRepo := reposynonym.New(esClient)
	synonymUC := ucsynonym.New(synonymRepo)

	jobHandlers := map[string]ucworker.Handler{
		ucworker.JobTypeGenomicFNA:   handlers.NewGenomicFNAHandler(),
		ucworker.JobTypeGenomicGFF:   handlers.NewGenomicGFFHandler(versionRepo, genomicUC, genomicRepo),
		ucworker.JobTypeRNAFNA:       handlers.NewRNAFNAHandler(versionRepo, sequenceUC, sequenceRepo),
		ucworker.JobTypeCDSFNA:       handlers.NewCDSFNAHandler(versionRepo, sequenceUC, sequenceRepo),
		ucworker.JobTypeProteinFAA:   handlers.NewProteinFAAHandler(versionRepo, sequenceUC, sequenceRepo),
		ucworker.JobTypeOrthologyTSV: handlers.NewOrthologyTSVHandler(versionRepo, orthologyUC, orthologyRepo),
		ucworker.JobTypeSynonym: handlers.NewSynonymHandler(
			versionRepo, synonymUC, synonymRepo,
			synonymparser.NewGFF3SynonymParser(config.MainSpecies),
			synonymparser.NewFlyBaseSynonymParser(config.MainSpecies),
			synonymparser.NewFlyBaseGeneRNAProteinMapParser(config.MainSpecies),
		),
	}

	w := ucworker.New(
		repojob.New(db),
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
