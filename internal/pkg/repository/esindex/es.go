package esindex

import (
	"context"
	"fmt"
	"strings"

	"github.com/elastic/go-elasticsearch/v8"
)

type Repository struct {
	esClient *elasticsearch.Client
}

func New(esClient *elasticsearch.Client) *Repository {
	return &Repository{esClient: esClient}
}

// DeleteIndexesByVersion deletes all Elasticsearch indexes associated with the
// given version across all index types (genomic, sequence, orthology, synonym).
// Missing indexes are ignored (ignore_unavailable=true).
func (r *Repository) DeleteIndexesByVersion(ctx context.Context, versionName string) error {
	vn := strings.ToLower(versionName)
	patterns := []string{
		"emobasegenomics-genomiclocation-" + vn + "-*",
		"emobasegenomics-sequence-" + vn + "-*",
		"emobasegenomics-orthology-" + vn + "-*",
		"emobasegenomics-synonym-" + vn + "-*",
	}

	res, err := r.esClient.Indices.Delete(
		patterns,
		r.esClient.Indices.Delete.WithContext(ctx),
		r.esClient.Indices.Delete.WithIgnoreUnavailable(true),
	)
	if err != nil {
		return fmt.Errorf("failed to delete ES indexes for version %q: %w", versionName, err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.IsError() {
		return fmt.Errorf("elasticsearch delete indexes failed for version %q: %s", versionName, res.String())
	}
	return nil
}
