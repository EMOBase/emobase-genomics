package main

import (
	"context"
	"strings"

	"github.com/elastic/go-elasticsearch/v8"
)

func esMigrate(_ context.Context, esClient *elasticsearch.Client) error {
	// Delete all existing indices. Development purpose only.
	_, err := esClient.Indices.Delete(
		[]string{"genomiclocation", "sequence", "orthology", "synonym"},
		esClient.Indices.Delete.WithIgnoreUnavailable(true),
	)
	if err != nil {
		return err
	}

	// Create indices with mappings
	sequenceMapping := `{
		"mappings": {
			"properties": {
				"name": {"type": "text"},
				"species": {"type": "text"},
				"sequence": {"type": "text", "index": false},
				"type": {"type": "text"}
			}
		}
	}`
	_, err = esClient.Indices.Create("sequence",
		esClient.Indices.Create.WithBody(strings.NewReader(sequenceMapping)))
	if err != nil {
		return err
	}

	synonymMapping := `{
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
						"keyword": { "type": "keyword" },
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
	}`
	_, err = esClient.Indices.Create("synonym",
		esClient.Indices.Create.WithBody(strings.NewReader(synonymMapping)))
	if err != nil {
		return err
	}

	return nil
}
