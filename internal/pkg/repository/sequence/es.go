package sequence

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

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
	sequences []entity.Sequence,
) error {
	for i := 0; i < len(sequences); i += r.batchSize {
		end := min(i+r.batchSize, len(sequences))
		if err := r.bulkIndex(ctx, indexName, sequences[i:end]); err != nil {
			return err
		}
	}
	return nil
}

func (r *ElasticSearchRepository) bulkIndex(
	ctx context.Context,
	indexName string,
	sequences []entity.Sequence,
) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)

	for _, sequence := range sequences {
		meta := map[string]map[string]string{
			"index": {"_id": sequence.GetID(), "_index": indexName},
		}
		if err := enc.Encode(meta); err != nil {
			return err
		}
		if err := enc.Encode(sequence); err != nil {
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

// FindByIDs returns sequence documents whose ES document ID matches any of the given IDs.
func (r *ElasticSearchRepository) FindByIDs(ctx context.Context, indexName string, ids []string) ([]entity.Sequence, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	body, err := json.Marshal(map[string]any{
		"query": map[string]any{"ids": map[string]any{"values": ids}},
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
				Source entity.Sequence `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode search response: %w", err)
	}

	sequences := make([]entity.Sequence, len(result.Hits.Hits))
	for i, h := range result.Hits.Hits {
		sequences[i] = h.Source
	}
	return sequences, nil
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
