package configs

import (
	"errors"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/spf13/viper"
)

var config *Config

type Config struct {
	MainSpecies   string              `mapstructure:"main_species"`
	HTTP          HTTPConfig          `mapstructure:"http"`
	MySQL         MySQLConfig         `mapstructure:"mysql"`
	Jobs          JobsConfig          `mapstructure:"jobs"`
	Elasticsearch ElasticsearchConfig `mapstructure:"elasticsearch"`
	Keycloak      KeycloakConfig      `mapstructure:"keycloak"`
	Blast         BlastConfig         `mapstructure:"blast"`
	Uploads       UploadsConfig       `mapstructure:"uploads"`
}

type KeycloakConfig struct {
	URL           string `mapstructure:"url"`
	Realm         string `mapstructure:"realm"`
	Issuer        string `mapstructure:"issuer"`
	RequiredRole  string `mapstructure:"required_role"`
	DevBypassAuth bool   `mapstructure:"dev_bypass_auth"`
}

type UploadsConfig struct {
	Dir string `mapstructure:"dir"`
}

type BlastConfig struct {
	// DisplayName is used as the human-readable title prefix in makeblastdb
	// (e.g. "Drosophila melanogaster" → "Drosophila melanogaster Genome").
	DisplayName string `mapstructure:"display_name"`
	// DBPath is the directory where makeblastdb writes its output databases.
	DBPath string `mapstructure:"db_path"`
	// ContainerName is the Docker container name of the blast service to restart
	// after all blast databases are rebuilt.
	ContainerName string `mapstructure:"container_name"`
}

type ElasticsearchConfig struct {
	Addresses     []string `mapstructure:"addresses"`
	IndexPrefix   string   `mapstructure:"index_prefix"`
	BulkBatchSize int      `mapstructure:"bulk_batch_size"`
}

type JobsConfig struct {
	PollInterval  time.Duration `mapstructure:"poll_interval"`
	StuckInterval time.Duration `mapstructure:"stuck_interval"`
	StuckTimeout  time.Duration `mapstructure:"stuck_timeout"`
}

type HTTPConfig struct {
	Port int    `mapstructure:"port"`
	Mode string `mapstructure:"mode"`
}

type MySQLConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password" json:"-"`
	Database string `mapstructure:"database"`
}

func LoadConfig(path string) (*Config, error) {
	if path != "" {
		viper.SetConfigFile(path)
	}

	viper.SetConfigType("yaml")
	if err := viper.ReadInConfig(); err != nil {
		log.Warn().Err(err).Msg("failed to read configuration file")
		return nil, err
	}

	envPrefix := "emobase_genomics"
	if v, ok := os.LookupEnv("EMOBASE_GENOMICS_ENV_PREFIX"); ok {
		envPrefix = v
	}

	viper.SetEnvPrefix(envPrefix)
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "__", "-", "_"))
	viper.AutomaticEnv()

	if err := viper.Unmarshal(&config); err != nil {
		log.Err(err).
			Msg("failed to unmarshal configuration")

		panic(err)
	}

	if config.MainSpecies == "" {
		return nil, errors.New("main_species is required but not set")
	}

	log.Info().Any("config", config).Msg("configuration loaded successfully")

	return config, nil
}
