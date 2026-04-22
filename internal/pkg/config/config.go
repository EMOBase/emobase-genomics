package configs

import (
	"strings"
	"time"

	"github.com/rs/zerolog"
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
	Blast         BlastConfig         `mapstructure:"blast"`
	Uploads       UploadsConfig       `mapstructure:"uploads"`
	Dev           DevConfig           `mapstructure:"dev"`
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
}

type DevConfig struct {
	// UploadChunkDelay adds an artificial delay after each uploaded chunk.
	// Set to a non-zero value (e.g. "500ms") to simulate slow uploads during development.
	UploadChunkDelay time.Duration `mapstructure:"upload_chunk_delay"`
}

type ElasticsearchConfig struct {
	Addresses []string `mapstructure:"addresses"`
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
	Password string `mapstructure:"password"`
	Database string `mapstructure:"database"`
}

func (c MySQLConfig) MarshalZerologObject(e *zerolog.Event) {
	e.Str("host", c.Host).
		Int("port", c.Port).
		Str("user", c.User).
		Str("database", c.Database)
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

	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "__", "-", "_"))
	viper.AutomaticEnv()

	if err := viper.Unmarshal(&config); err != nil {
		log.Err(err).
			Msg("failed to unmarshal configuration")

		panic(err)
	}

	log.Info().Any("config", config).Msg("configuration loaded successfully")

	return config, nil
}
