package esindex

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/elastic/go-elasticsearch/v8"
)

type Repository struct {
	esClient *elasticsearch.Client
	prefix   string
}

func New(esClient *elasticsearch.Client, prefix string) *Repository {
	return &Repository{esClient: esClient, prefix: prefix}
}

// DeleteIndexesByVersion deletes all Elasticsearch indexes associated with the
// given version across all index types (genomic, sequence, orthology, synonym, dsrna).
//
// ES 8 sets action.destructive_requires_name=true by default, which rejects wildcard
// expressions in DELETE. We resolve wildcards with a GET first, then delete by
// explicit name.
func (r *Repository) DeleteIndexesByVersion(ctx context.Context, versionName string) error {
	vn := strings.ToLower(versionName)
	p := r.prefix + "-"
	patterns := []string{
		p + "genomiclocation-" + vn + "-*",
		p + "sequence-" + vn + "-*",
		p + "orthology-" + vn + "-*",
		p + "synonym-" + vn + "-*",
		p + "dsrna-" + vn + "-*",
	}

	// Step 1: resolve wildcard patterns to concrete index names.
	// GET supports wildcards regardless of action.destructive_requires_name.
	getRes, err := r.esClient.Indices.Get(
		patterns,
		r.esClient.Indices.Get.WithContext(ctx),
		r.esClient.Indices.Get.WithAllowNoIndices(true),
		r.esClient.Indices.Get.WithIgnoreUnavailable(true),
	)
	if err != nil {
		return fmt.Errorf("failed to list ES indexes for version %q: %w", versionName, err)
	}
	defer func() { _ = getRes.Body.Close() }()

	if getRes.StatusCode == http.StatusNotFound {
		return nil
	}
	if getRes.IsError() {
		return fmt.Errorf("elasticsearch list indexes failed for version %q: %s", versionName, getRes.String())
	}

	var indices map[string]json.RawMessage
	if err := json.NewDecoder(getRes.Body).Decode(&indices); err != nil {
		return fmt.Errorf("failed to decode index list for version %q: %w", versionName, err)
	}
	if len(indices) == 0 {
		return nil
	}

	// Step 2: delete by explicit names — no wildcards, safe under any ES security setting.
	names := make([]string, 0, len(indices))
	for name := range indices {
		names = append(names, name)
	}

	delRes, err := r.esClient.Indices.Delete(
		names,
		r.esClient.Indices.Delete.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("failed to delete ES indexes for version %q: %w", versionName, err)
	}
	defer func() { _ = delRes.Body.Close() }()

	if delRes.IsError() {
		return fmt.Errorf("elasticsearch delete indexes failed for version %q: %s", versionName, delRes.String())
	}
	return nil
}
