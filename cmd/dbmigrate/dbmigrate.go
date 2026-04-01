package dbmigrate

import (
	"context"
	"errors"

	configs "github.com/EMOBase/emobase-genomics/internal/pkg/config"
	"github.com/EMOBase/emobase-genomics/internal/pkg/database"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/mysql"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v3"
)

func Action(ctx context.Context, cmd *cli.Command) error {
	configFile := cmd.String("config")
	config, err := configs.LoadConfig(configFile)
	if err != nil {
		return err
	}

	direction := cmd.String("direction")

	m, err := migrate.New("file://migrations", database.MySQLMigrateDSN(config.MySQL))
	if err != nil {
		return err
	}
	defer m.Close()

	switch direction {
	case "up":
		err = m.Up()
	case "down":
		err = m.Down()
	default:
		return errors.New("invalid direction: must be 'up' or 'down'")
	}

	if errors.Is(err, migrate.ErrNoChange) {
		log.Info().Msg("no migrations to apply")
		return nil
	}

	if err != nil {
		return err
	}

	log.Info().Str("direction", direction).Msg("migrations applied successfully")
	return nil
}
