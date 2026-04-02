package database

import (
	configs "github.com/EMOBase/emobase-genomics/internal/pkg/config"
	"github.com/elastic/go-elasticsearch/v8"
)

func NewElasticsearch(cfg configs.ElasticsearchConfig) (*elasticsearch.Client, error) {
	return elasticsearch.NewClient(elasticsearch.Config{
		Addresses: cfg.Addresses,
	})
}
