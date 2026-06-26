package orthology

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
	orthologies []entity.Orthology,
) error {
	for i := 0; i < len(orthologies); i += r.batchSize {
		end := min(i+r.batchSize, len(orthologies))
		if err := r.bulkIndex(ctx, indexName, orthologies[i:end]); err != nil {
			return err
		}
	}
	return nil
}

func (r *ElasticSearchRepository) bulkIndex(
	ctx context.Context,
	indexName string,
	orthologies []entity.Orthology,
) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)

	for _, orthology := range orthologies {
		meta := map[string]map[string]string{
			"index": {"_id": orthology.GetID(), "_index": indexName},
		}
		if err := enc.Encode(meta); err != nil {
			return err
		}
		if err := enc.Encode(orthology); err != nil {
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

// ListByOrthologs returns all orthology groups that contain any of the given gene IDs.
func (r *ElasticSearchRepository) ListByOrthologs(ctx context.Context, indexName string, genes []string) ([]entity.Orthology, error) {
	if len(genes) == 0 {
		return nil, nil
	}
	body, err := json.Marshal(map[string]any{
		"query": map[string]any{"terms": map[string]any{"orthologs": genes}},
		"sort":  []map[string]any{{"group": map[string]string{"order": "asc"}}},
		"size":  1000,
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
				Source entity.Orthology `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode search response: %w", err)
	}

	orthologies := make([]entity.Orthology, len(result.Hits.Hits))
	for i, h := range result.Hits.Hits {
		orthologies[i] = h.Source
	}
	return orthologies, nil
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
