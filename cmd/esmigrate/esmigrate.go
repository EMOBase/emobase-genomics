package esmigrate

import (
	"context"
	"strings"

	configs "github.com/EMOBase/emobase-genomics/internal/pkg/config"
	"github.com/EMOBase/emobase-genomics/internal/pkg/database"
	"github.com/elastic/go-elasticsearch/v8"
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v3"
)

func Action(ctx context.Context, cmd *cli.Command) error {
	configFile := cmd.String("config")
	config, err := configs.LoadConfig(configFile)
	if err != nil {
		return err
	}

	esClient, err := database.NewElasticsearch(config.Elasticsearch)
	if err != nil {
		return err
	}

	if err := run(ctx, esClient); err != nil {
		return err
	}

	log.Info().Msg("elasticsearch migrations applied successfully")
	return nil
}

func run(ctx context.Context, esClient *elasticsearch.Client) error {
	return createIndexTemplates(ctx, esClient)
}

// createIndexTemplates registers composable index templates so any dynamically
// created index matching the pattern inherits the correct mappings automatically.
func createIndexTemplates(ctx context.Context, esClient *elasticsearch.Client) error {
	templates := []struct {
		name string
		body string
	}{
		{
			name: "emobasegenomics-sequence",
			body: `{
				"index_patterns": ["emobasegenomics-sequence-*"],
				"priority": 100,
				"template": {
					"mappings": {
						"properties": {
							"name":     {"type": "text"},
							"species":  {"type": "keyword"},
							"sequence": {"type": "text", "index": false},
							"type":     {"type": "keyword"}
						}
					}
				}
			}`,
		},
		{
			name: "emobasegenomics-synonym",
			body: `{
				"index_patterns": ["emobasegenomics-synonym-*"],
				"priority": 100,
				"template": {
					"settings": {
						"analysis": {
							"analyzer": {
								"synonym_analyzer": {
									"type": "custom",
									"tokenizer": "keyword",
									"filter": ["lowercase", "synonym_filter"]
								}
							},
							"filter": {
								"synonym_filter": {
									"type": "pattern_replace",
									"pattern": "-",
									"replacement": " ",
									"all": true
								}
							}
						}
					},
					"mappings": {
						"properties": {
							"synonym": {
								"type": "text",
								"analyzer": "synonym_analyzer",
								"fields": {
									"keyword": {"type": "keyword"},
									"suggest": {
										"type": "completion",
										"analyzer": "synonym_analyzer",
										"preserve_separators": true,
										"preserve_position_increments": true,
										"max_input_length": 50,
										"contexts": [{
											"name": "synonym_type",
											"type": "category",
											"path": "type"
										}]
									}
								}
							},
							"gene": {"type": "text"},
							"type": {"type": "text"}
						}
					}
				}
			}`,
		},
	}

	for _, t := range templates {
		res, err := esClient.Indices.PutIndexTemplate(
			t.name,
			strings.NewReader(t.body),
			esClient.Indices.PutIndexTemplate.WithContext(ctx),
		)
		if err != nil {
			return err
		}
		_ = res.Body.Close()

		if res.IsError() {
			return errorFromResponse("put index template "+t.name, res.String())
		}

		log.Info().Str("template", t.name).Msg("index template applied")
	}

	return nil
}

func errorFromResponse(op, body string) error {
	return &esError{op: op, body: body}
}

type esError struct {
	op   string
	body string
}

func (e *esError) Error() string {
	return e.op + ": " + e.body
}
