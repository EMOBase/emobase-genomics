package configs

import (
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/spf13/viper"
)

var config *Config

type Config struct {
	HTTP HTTPConfig `mapstructure:"http"`
}

type HTTPConfig struct {
	Port int    `mapstructure:"port"`
	Mode string `mapstructure:"mode"`
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
