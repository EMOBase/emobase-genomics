package esmigrate

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"
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

	if err := run(ctx, esClient, config.Elasticsearch.IndexPrefix); err != nil {
		return err
	}

	log.Info().Msg("elasticsearch migrations applied successfully")
	return nil
}

func run(ctx context.Context, esClient *elasticsearch.Client, indexPrefix string) error {
	if err := createIndexTemplates(ctx, esClient, indexPrefix); err != nil {
		return err
	}
	return addSynonymSpeciesType(ctx, esClient, indexPrefix)
}

// synonymProperties are the synonym index fields: the template's, for new
// indexes, and put on every existing synonym index by addSynonymSpeciesType.
// ES rejects a put that changes an existing field, so both use this one copy.
// species_type is "{species}|{type}" (see the synonym repository's speciesType),
// the one context of suggest_species: ES matches a suggestion that has any one
// of the query's contexts, so species and type can't be two contexts.
const synonymProperties = `{
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
				"contexts": [{"name": "synonym_type", "type": "category", "path": "type"}]
			},
			"suggest_species": {
				"type": "completion",
				"analyzer": "synonym_analyzer",
				"preserve_separators": true,
				"preserve_position_increments": true,
				"max_input_length": 50,
				"contexts": [{"name": "species_type", "type": "category", "path": "species_type"}]
			}
		}
	},
	"gene": {"type": "keyword"},
	"type": {"type": "keyword"},
	"species_type": {"type": "keyword"}
}`

// speciesTypeScript sets a synonym's species_type exactly as the synonym
// repository writes it: its gene's species prefix ("" if none), "|", its type.
const speciesTypeScript = `String g = ctx._source.gene; int i = g == null ? -1 : g.indexOf(':'); ` +
	`ctx._source.species_type = (i < 0 ? '' : g.substring(0, i)) + '|' + ctx._source.type;`

// addSynonymSpeciesType brings synonym indexes created before species_type
// existed up to synonymProperties: it adds the new fields to every one's
// mapping, then fills species_type in place on the documents without it. Every
// mapping goes first, so a species-filtered suggestion never meets an index
// without synonym.suggest_species; until the fill ends it only misses some
// documents. Done indexes have nothing to fill, so a rerun changes nothing.
func addSynonymSpeciesType(ctx context.Context, esClient *elasticsearch.Client, indexPrefix string) error {
	indexes, err := listIndexes(ctx, esClient, indexPrefix+"-synonym-*")
	if err != nil {
		return err
	}

	mapping := `{"properties": ` + synonymProperties + `}`
	for _, index := range indexes {
		res, err := esClient.Indices.PutMapping(
			[]string{index},
			strings.NewReader(mapping),
			esClient.Indices.PutMapping.WithContext(ctx),
		)
		if err != nil {
			return err
		}
		_ = res.Body.Close()
		if res.IsError() {
			return errorFromResponse("put mapping "+index, res.String())
		}
	}

	body, err := json.Marshal(map[string]any{
		"query":  map[string]any{"bool": map[string]any{"must_not": map[string]any{"exists": map[string]any{"field": "species_type"}}}},
		"script": map[string]any{"lang": "painless", "source": speciesTypeScript},
	})
	if err != nil {
		return err
	}
	for _, index := range indexes {
		res, err := esClient.UpdateByQuery(
			[]string{index},
			esClient.UpdateByQuery.WithContext(ctx),
			esClient.UpdateByQuery.WithBody(bytes.NewReader(body)),
			esClient.UpdateByQuery.WithConflicts("proceed"),
			esClient.UpdateByQuery.WithSlices("auto"),
			esClient.UpdateByQuery.WithRefresh(true),
		)
		if err != nil {
			return err
		}
		var result struct {
			Updated          int               `json:"updated"`
			VersionConflicts int               `json:"version_conflicts"`
			Failures         []json.RawMessage `json:"failures"`
		}
		if res.IsError() {
			_ = res.Body.Close()
			return errorFromResponse("fill species_type in "+index, res.String())
		}
		err = json.NewDecoder(res.Body).Decode(&result)
		_ = res.Body.Close()
		if err != nil {
			return err
		}
		if len(result.Failures) > 0 {
			return errorFromResponse("fill species_type in "+index, string(result.Failures[0]))
		}
		// A conflict is a document written meanwhile: by current code, with its
		// species_type; by an older worker, without, until es:migrate runs again.
		log.Info().Str("index", index).Int("updated", result.Updated).
			Int("versionConflicts", result.VersionConflicts).Msg("synonym species_type filled")
	}
	return nil
}

// listIndexes resolves pattern to the concrete index names it matches.
func listIndexes(ctx context.Context, esClient *elasticsearch.Client, pattern string) ([]string, error) {
	res, err := esClient.Indices.Get(
		[]string{pattern},
		esClient.Indices.Get.WithContext(ctx),
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.IsError() {
		return nil, errorFromResponse("get indexes "+pattern, res.String())
	}
	var indexes map[string]json.RawMessage
	if err := json.NewDecoder(res.Body).Decode(&indexes); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(indexes))
	for name := range indexes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// createIndexTemplates registers composable index templates so any dynamically
// created index matching the pattern inherits the correct mappings automatically.
func createIndexTemplates(ctx context.Context, esClient *elasticsearch.Client, indexPrefix string) error {
	p := indexPrefix + "-"
	templates := []struct {
		name string
		body string
	}{
		{
			name: p + "sequence",
			body: `{
				"index_patterns": ["` + p + `sequence-*"],
				"priority": 100,
				"template": {
					"mappings": {
						"properties": {
							"name":     {"type": "text"},
							"species":  {"type": "keyword"},
							"sequence": {"type": "text", "index": false},
							"type":     {"type": "keyword"},
							"file_id":  {"type": "keyword"}
						}
					}
				}
			}`,
		},
		{
			name: p + "orthology",
			body: `{
				"index_patterns": ["` + p + `orthology-*"],
				"priority": 100,
				"template": {
					"mappings": {
						"properties": {
							"group":     { "type": "keyword" },
							"orthologs": { "type": "keyword" },
							"file_id":   { "type": "keyword" }
						}
					}
				}
			}`,
		},
		{
			name: p + "dsrna",
			body: `{
				"index_patterns": ["` + p + `dsrna-*"],
				"priority": 100,
				"template": {
					"mappings": {
						"properties": {
							"gene":        {"type": "keyword"},
							"seq":         {"type": "text", "index": false},
							"leftPrimer":  {"type": "text", "index": false},
							"rightPrimer": {"type": "text", "index": false}
						}
					}
				}
			}`,
		},
		{
			name: p + "synonym",
			body: `{
				"index_patterns": ["` + p + `synonym-*"],
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
					"mappings": {"properties": ` + synonymProperties + `}
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
