package genomic

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
	locs []entity.GenomicLocation,
) error {
	for i := 0; i < len(locs); i += r.batchSize {
		end := min(i+r.batchSize, len(locs))
		if err := r.bulkIndex(ctx, indexName, locs[i:end]); err != nil {
			return err
		}
	}
	return nil
}

func (r *ElasticSearchRepository) bulkIndex(
	ctx context.Context,
	indexName string,
	locs []entity.GenomicLocation,
) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)

	for _, loc := range locs {
		meta := map[string]map[string]string{
			"index": {"_id": loc.GetID(), "_index": indexName},
		}
		if err := enc.Encode(meta); err != nil {
			return err
		}
		if err := enc.Encode(loc); err != nil {
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

// FindByIDs returns genomic location documents whose ES document ID matches any of the given IDs.
func (r *ElasticSearchRepository) FindByIDs(ctx context.Context, indexName string, ids []string) ([]entity.GenomicLocation, error) {
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
				Source entity.GenomicLocation `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode search response: %w", err)
	}

	locs := make([]entity.GenomicLocation, len(result.Hits.Hits))
	for i, h := range result.Hits.Hits {
		locs[i] = h.Source
	}
	return locs, nil
}

// DeleteStaleIndexes deletes all indexes matching the pattern aliasName-* except
// liveIndexName, cleaning up old timestamped indexes after a re-upload.
func (r *ElasticSearchRepository) DeleteStaleIndexes(ctx context.Context, aliasName, liveIndexName string) error {
	pattern := aliasName + "-*"

	getRes, err := r.esClient.Indices.Get(
		[]string{pattern},
		r.esClient.Indices.Get.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("failed to list indexes for pattern %q: %w", pattern, err)
	}
	defer func() { _ = getRes.Body.Close() }()

	if getRes.StatusCode == http.StatusNotFound {
		return nil
	}
	if getRes.IsError() {
		return fmt.Errorf("elasticsearch list indexes failed: %s", getRes.String())
	}

	var indices map[string]json.RawMessage
	if err := json.NewDecoder(getRes.Body).Decode(&indices); err != nil {
		return fmt.Errorf("failed to decode index list: %w", err)
	}

	var toDelete []string
	for name := range indices {
		if name != liveIndexName {
			toDelete = append(toDelete, name)
		}
	}
	if len(toDelete) == 0 {
		return nil
	}

	delRes, err := r.esClient.Indices.Delete(
		toDelete,
		r.esClient.Indices.Delete.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("failed to delete stale indexes: %w", err)
	}
	defer func() { _ = delRes.Body.Close() }()

	if delRes.IsError() {
		return fmt.Errorf("elasticsearch delete indexes failed: %s", delRes.String())
	}
	return nil
}

// SetAlias atomically points aliasName to indexName,
// removing it from any previous index it may have pointed to.
func (r *ElasticSearchRepository) SetAlias(ctx context.Context, indexName, aliasName string) error {
	actions := []map[string]any{}

	// Find any existing indices that currently hold the alias so we can
	// remove them atomically in the same request.
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
