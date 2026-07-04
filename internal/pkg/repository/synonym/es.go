package synonym

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	"github.com/elastic/go-elasticsearch/v8"
)

type ElasticSearchRepository struct {
	esClient  *elasticsearch.Client
	batchSize int
}

func New(esClient *elasticsearch.Client, batchSize int) *ElasticSearchRepository {
	return &ElasticSearchRepository{esClient: esClient, batchSize: batchSize}
}

func (r *ElasticSearchRepository) SaveMany(
	ctx context.Context,
	indexName string,
	synonyms []entity.Synonym,
) error {
	for i := 0; i < len(synonyms); i += r.batchSize {
		end := min(i+r.batchSize, len(synonyms))
		if err := r.bulkIndex(ctx, indexName, synonyms[i:end]); err != nil {
			return err
		}
	}
	return nil
}

func (r *ElasticSearchRepository) bulkIndex(
	ctx context.Context,
	indexName string,
	synonyms []entity.Synonym,
) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)

	for _, synonym := range synonyms {
		meta := map[string]map[string]string{
			"index": {"_id": synonym.GetID(), "_index": indexName},
		}
		if err := enc.Encode(meta); err != nil {
			return err
		}
		if err := enc.Encode(synonym); err != nil {
			return err
		}
	}

	res, err := r.esClient.Bulk(
		bytes.NewReader(buf.Bytes()),
		r.esClient.Bulk.WithContext(ctx),
	)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()

	if res.IsError() {
		return fmt.Errorf("elasticsearch bulk request failed: %s", res.String())
	}

	var result struct {
		Errors bool `json:"errors"`
	}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode bulk response: %w", err)
	}
	if result.Errors {
		return fmt.Errorf("elasticsearch bulk index had partial failures for index %q", indexName)
	}

	return nil
}

// DeleteByFileID removes all documents in indexName where file_id equals fileID.
func (r *ElasticSearchRepository) DeleteByFileID(ctx context.Context, indexName, fileID string) error {
	query := `{"query":{"term":{"file_id":"` + fileID + `"}}}`
	res, err := r.esClient.DeleteByQuery(
		[]string{indexName},
		strings.NewReader(query),
		r.esClient.DeleteByQuery.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("failed to delete by file_id: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.IsError() {
		return fmt.Errorf("elasticsearch delete_by_query failed: %s", res.String())
	}
	return nil
}

// SetAlias atomically points aliasName to indexName,
// removing it from any previous index it may have pointed to.
func (r *ElasticSearchRepository) SetAlias(ctx context.Context, indexName, aliasName string) error {
	actions := []map[string]any{}

	getRes, err := r.esClient.Indices.GetAlias(
		r.esClient.Indices.GetAlias.WithContext(ctx),
		r.esClient.Indices.GetAlias.WithName(aliasName),
	)
	if err != nil {
		return fmt.Errorf("failed to query alias %q: %w", aliasName, err)
	}
	defer func() { _ = getRes.Body.Close() }()

	if getRes.StatusCode == http.StatusNotFound {
		// Alias does not exist yet — nothing to remove.
	} else if getRes.IsError() {
		return fmt.Errorf("elasticsearch get alias failed: %s", getRes.String())
	} else {
		var current map[string]json.RawMessage
		if err := json.NewDecoder(getRes.Body).Decode(&current); err != nil {
			return fmt.Errorf("failed to decode alias response: %w", err)
		}
		for index := range current {
			actions = append(actions, map[string]any{
				"remove": map[string]string{"index": index, "alias": aliasName},
			})
		}
	}

	actions = append(actions, map[string]any{
		"add": map[string]string{"index": indexName, "alias": aliasName},
	})

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(map[string]any{"actions": actions}); err != nil {
		return fmt.Errorf("failed to encode alias actions: %w", err)
	}

	updateRes, err := r.esClient.Indices.UpdateAliases(
		bytes.NewReader(buf.Bytes()),
		r.esClient.Indices.UpdateAliases.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("failed to update aliases: %w", err)
	}
	defer func() { _ = updateRes.Body.Close() }()

	if updateRes.IsError() {
		return fmt.Errorf("elasticsearch update aliases failed: %s", updateRes.String())
	}

	return nil
}

// FindBySynonymRelaxed returns synonyms whose synonym field matches query using
// a full-text match query (case-insensitive, hyphen-tolerant via the synonym_analyzer).
func (r *ElasticSearchRepository) FindBySynonymRelaxed(ctx context.Context, indexName, query string) ([]entity.Synonym, error) {
	body, err := json.Marshal(map[string]any{
		"query": map[string]any{"match": map[string]any{"synonym": query}},
		"size":  1000,
	})
	if err != nil {
		return nil, err
	}
	return r.searchSynonyms(ctx, indexName, body)
}

// FindBySynonyms returns all synonym documents whose synonym field exactly matches any of the given values.
func (r *ElasticSearchRepository) FindBySynonyms(ctx context.Context, indexName string, synonyms []string) ([]entity.Synonym, error) {
	if len(synonyms) == 0 {
		return nil, nil
	}
	body, err := json.Marshal(map[string]any{
		"query": map[string]any{"terms": map[string]any{"synonym.keyword": synonyms}},
		"size":  1000,
	})
	if err != nil {
		return nil, err
	}
	return r.searchSynonyms(ctx, indexName, body)
}

// FindByGenes returns all synonym documents whose gene field matches any of the given gene IDs.
func (r *ElasticSearchRepository) FindByGenes(ctx context.Context, indexName string, genes []string) ([]entity.Synonym, error) {
	if len(genes) == 0 {
		return nil, nil
	}
	body, err := json.Marshal(map[string]any{
		"query": map[string]any{"terms": map[string]any{"gene": genes}},
		"size":  1000,
	})
	if err != nil {
		return nil, err
	}
	return r.searchSynonyms(ctx, indexName, body)
}

// Suggest returns up to 100 autocomplete suggestions for the given prefix,
// ranked by synonym type (SYMBOL > NAME > CURRENT_ID/DSRNA/OLD_ID > OTHER).
func (r *ElasticSearchRepository) Suggest(ctx context.Context, indexName, prefix string) ([]string, error) {
	body, err := json.Marshal(map[string]any{
		"size": 0,
		"suggest": map[string]any{
			"suggest": map[string]any{
				"prefix": prefix,
				"completion": map[string]any{
					"field":           "synonym.suggest",
					"size":            100,
					"skip_duplicates": true,
					"contexts": map[string]any{
						"synonym_type": []map[string]any{
							{"context": "SYMBOL", "boost": 12},
							{"context": "NAME", "boost": 8},
							{"context": "CURRENT_ID", "boost": 4},
							{"context": "DSRNA", "boost": 4},
							{"context": "OLD_ID", "boost": 4},
							{"context": "OTHER", "boost": 1},
						},
					},
				},
			},
		},
	})
	if err != nil {
		return nil, err
	}

	res, err := r.esClient.Search(
		r.esClient.Search.WithContext(ctx),
		r.esClient.Search.WithIndex(indexName),
		r.esClient.Search.WithBody(bytes.NewReader(body)),
	)
	if err != nil {
		return nil, fmt.Errorf("elasticsearch suggest failed: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if res.IsError() {
		return nil, fmt.Errorf("elasticsearch suggest failed: %s", res.String())
	}

	var result struct {
		Suggest map[string][]struct {
			Options []struct {
				Text string `json:"text"`
			} `json:"options"`
		} `json:"suggest"`
	}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode suggest response: %w", err)
	}

	var suggestions []string
	for _, hit := range result.Suggest["suggest"] {
		for _, opt := range hit.Options {
			suggestions = append(suggestions, opt.Text)
		}
	}
	return suggestions, nil
}

func (r *ElasticSearchRepository) searchSynonyms(ctx context.Context, indexName string, body []byte) ([]entity.Synonym, error) {
	res, err := r.esClient.Search(
		r.esClient.Search.WithContext(ctx),
		r.esClient.Search.WithIndex(indexName),
		r.esClient.Search.WithBody(bytes.NewReader(body)),
	)
	if err != nil {
		return nil, fmt.Errorf("elasticsearch search failed: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if res.IsError() {
		return nil, fmt.Errorf("elasticsearch search failed: %s", res.String())
	}

	var result struct {
		Hits struct {
			Hits []struct {
				Source entity.Synonym `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode search response: %w", err)
	}

	synonyms := make([]entity.Synonym, len(result.Hits.Hits))
	for i, h := range result.Hits.Hits {
		synonyms[i] = h.Source
	}
	return synonyms, nil
}
