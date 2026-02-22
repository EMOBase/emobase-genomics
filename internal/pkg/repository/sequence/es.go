package sequence

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	"github.com/elastic/go-elasticsearch/v8"
)

type ElasticSearchRepository struct {
	esClient  *elasticsearch.Client
	indexName string
}

func NewElasticSearchRepository(
	esClient *elasticsearch.Client,
	indexName string,
) *ElasticSearchRepository {
	return &ElasticSearchRepository{esClient: esClient, indexName: indexName}
}

func (r *ElasticSearchRepository) SaveMany(
	ctx context.Context,
	sequences []entity.Sequence,
) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)

	for _, sequence := range sequences {
		meta := map[string]map[string]string{
			"index": {
				"_id":    sequence.GetID(),
				"_index": r.indexName,
			},
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

	defer res.Body.Close()

	// TODO: why?
	if res.IsError() {
		panic(res.String())
	}

	return nil
}
