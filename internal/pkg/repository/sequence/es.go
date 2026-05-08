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
