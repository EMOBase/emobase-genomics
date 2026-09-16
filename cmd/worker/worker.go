package worker

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	configs "github.com/EMOBase/emobase-genomics/internal/pkg/config"
	"github.com/EMOBase/emobase-genomics/internal/pkg/database"
	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	repoappsettings "github.com/EMOBase/emobase-genomics/internal/pkg/repository/appsettings"
	repoassemblyversion "github.com/EMOBase/emobase-genomics/internal/pkg/repository/assemblyversion"
	repodsrna "github.com/EMOBase/emobase-genomics/internal/pkg/repository/dsrna"
	repogenomic "github.com/EMOBase/emobase-genomics/internal/pkg/repository/genomic"
	repojob "github.com/EMOBase/emobase-genomics/internal/pkg/repository/job"
	repoorthology "github.com/EMOBase/emobase-genomics/internal/pkg/repository/orthology"
	reposequence "github.com/EMOBase/emobase-genomics/internal/pkg/repository/sequence"
	reposynonym "github.com/EMOBase/emobase-genomics/internal/pkg/repository/synonym"
	repouploadfile "github.com/EMOBase/emobase-genomics/internal/pkg/repository/uploadfile"
	repoversion "github.com/EMOBase/emobase-genomics/internal/pkg/repository/version"
	ucdsrna "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/dsrna"
	ucgenomic "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/genomic"
	ucorthology "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/orthology"
	ucsequence "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/sequence"
	ucsynonym "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/synonym"
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
	defer func() { _ = db.Close() }()

	esClient, err := database.NewElasticsearch(config.Elasticsearch)
	if err != nil {
		return err
	}

	versionRepo := repoversion.New(db)
	jobRepo := repojob.New(db)
	uploadFileRepo := repouploadfile.New(db)

	batchSize := config.Elasticsearch.BulkBatchSize

	genomicRepo := repogenomic.New(esClient, batchSize)
	genomicUC := ucgenomic.New(genomicRepo, batchSize)

	sequenceRepo := reposequence.New(esClient, batchSize)
	sequenceUC := ucsequence.New(sequenceRepo, batchSize)

	orthologyRepo := repoorthology.New(esClient, batchSize)
	orthologyUC := ucorthology.New(orthologyRepo, batchSize)

	synonymRepo := reposynonym.New(esClient, batchSize)
	synonymUC := ucsynonym.New(synonymRepo, batchSize)

	dsrnaRepo := repodsrna.New(esClient, batchSize)
	dsrnaUC := ucdsrna.New(dsrnaRepo, batchSize)

	blastDBPath := config.Blast.DBPath
	blastTitle := config.Blast.DisplayName
	blastContainerName := config.Blast.ContainerName
	indexPrefix := config.Elasticsearch.IndexPrefix
	appSettingsRepo := repoappsettings.New(db)
	assemblyVersionRepo := repoassemblyversion.New(db)

	synonymHandler := handlers.NewSynonymHandler(versionRepo, synonymUC, synonymRepo, indexPrefix)

	jobHandlers := map[string]ucworker.Handler{
		entity.JobTypeGenomicGFF:   handlers.NewGenomicGFFHandler(versionRepo, genomicUC, genomicRepo, indexPrefix),
		entity.JobTypeRNAFNA:       handlers.NewRNAFNAHandler(versionRepo, sequenceUC, sequenceRepo, indexPrefix),
		entity.JobTypeCDSFNA:       handlers.NewCDSFNAHandler(versionRepo, sequenceUC, sequenceRepo, indexPrefix),
		entity.JobTypeProteinFAA:   handlers.NewProteinFAAHandler(versionRepo, sequenceUC, sequenceRepo, indexPrefix),
		entity.JobTypeOrthologyTSV: handlers.NewOrthologyTSVHandler(versionRepo, orthologyUC, orthologyRepo, indexPrefix),
		entity.JobTypeOrthologyTSVDelete: handlers.NewDeleteOrthologyTSVHandler(
			config.Uploads.Dir, uploadFileRepo, versionRepo, orthologyRepo, indexPrefix,
		),
		entity.JobTypeSpeciesSynonym: synonymHandler,
		entity.JobTypeSpeciesSynonymDelete: handlers.NewDeleteSynonymHandler(
			config.Uploads.Dir, uploadFileRepo, versionRepo, jobRepo, synonymRepo, indexPrefix,
		),
		entity.JobTypeGenomicFNASetupBlast: handlers.NewSetupBlastHandler(
			"nucl", "Genome", "genome", blastDBPath, blastTitle, blastContainerName, jobRepo, appSettingsRepo, assemblyVersionRepo,
		),
		entity.JobTypeProteinFAASetupBlast: handlers.NewSetupBlastHandler(
			"prot", "Protein", "protein", blastDBPath, blastTitle, blastContainerName, jobRepo, appSettingsRepo, assemblyVersionRepo,
		),
		entity.JobTypeRNAFNASetupBlast: handlers.NewSetupBlastHandler(
			"nucl", "RNA", "rna", blastDBPath, blastTitle, blastContainerName, jobRepo, appSettingsRepo, assemblyVersionRepo,
		),
		entity.JobTypeProteinFAARemoveBlast: handlers.NewRemoveBlastHandler(
			"protein", blastDBPath, blastContainerName, jobRepo, appSettingsRepo, assemblyVersionRepo,
		),
		entity.JobTypeRNAFNARemoveBlast: handlers.NewRemoveBlastHandler(
			"rna", blastDBPath, blastContainerName, jobRepo, appSettingsRepo, assemblyVersionRepo,
		),
		entity.JobTypeGenomicFNASetupJBrowse2: handlers.NewSetupFNAJBrowse2Handler(jobRepo, config.JBrowse2.GeneLinkBase),
		entity.JobTypeGenomicGFFSetupJBrowse2: handlers.NewSetupGFFJBrowse2Handler(config.JBrowse2.GeneLinkBase),
		entity.JobTypeDsRNACSV:                handlers.NewDsRNACSVHandler(versionRepo, dsrnaUC, dsrnaRepo, indexPrefix),
		entity.JobTypeJBrowseTrack:            handlers.NewJBrowseTrackHandler(),
		entity.JobTypeJBrowseTrackDelete:      handlers.NewDeleteJBrowseTrackHandler(uploadFileRepo),
	}

	w := ucworker.New(
		repojob.New(db),
		jobHandlers,
		config.Jobs.PollInterval,
		config.Jobs.StuckInterval,
		config.Jobs.StuckTimeout,
		config.Jobs.MaxConcurrent,
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
