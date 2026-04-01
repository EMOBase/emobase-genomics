package configs

import (
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/spf13/viper"
)

var config *Config

type Config struct {
	HTTP  HTTPConfig  `mapstructure:"http"`
	MySQL MySQLConfig `mapstructure:"mysql"`
	Jobs  JobsConfig  `mapstructure:"jobs"`
}

type JobsConfig struct {
	MaxRetryCount int           `mapstructure:"max_retry_count"`
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
