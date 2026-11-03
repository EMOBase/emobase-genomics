package synonym

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	"github.com/EMOBase/emobase-genomics/internal/pkg/repository/esbulk"
	"github.com/elastic/go-elasticsearch/v8"
)

type ElasticSearchRepository struct {
	esClient  *elasticsearch.Client
	batchSize int
}

func New(esClient *elasticsearch.Client, batchSize int) *ElasticSearchRepository {
	return &ElasticSearchRepository{esClient: esClient, batchSize: batchSize}
}

// suggestTypeBoosts are the synonym types Suggest suggests, ranked by boost.
// Other types (TRANSCRIPT, PROTEIN) are never suggested.
var suggestTypeBoosts = []struct {
	synonymType string
	boost       int
}{
	{entity.SYNONYM_TYPE_SYMBOL, 12},
	{entity.SYNONYM_TYPE_NAME, 8},
	{entity.SYNONYM_TYPE_CURRENT_ID, 4},
	{entity.SYNONYM_TYPE_DSRNA, 4},
	{entity.SYNONYM_TYPE_OLD_ID, 4},
	{entity.SYNONYM_TYPE_OTHER, 1},
}

// synonymDoc is a synonym as stored: the synonym plus its species_type.
type synonymDoc struct {
	entity.Synonym
	SpeciesType string `json:"species_type"`
}

// speciesType is the species_type of a synonym, "{species}|{type}" (e.g.
// "Tcas|SYMBOL"): the one context of synonym.suggest_species, so a suggestion
// can be filtered by species and ranked by type at once. Two separate contexts
// would not do: ES matches a suggestion that has any one of the query's
// contexts. es:migrate's backfill script builds the same value.
func speciesType(species, synonymType string) string {
	return species + "|" + synonymType
}

// geneSpecies returns the species prefix of a "Species:GeneID" gene, or "" if
// it has none.
func geneSpecies(gene string) string {
	species, _, ok := strings.Cut(gene, ":")
	if !ok {
		return ""
	}
	return species
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
		if err := enc.Encode(synonymDoc{Synonym: synonym, SpeciesType: speciesType(geneSpecies(synonym.Gene), synonym.Type)}); err != nil {
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

	if err := esbulk.DecodeResponse(res.Body, indexName); err != nil {
		return err
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

// DeleteByFileIDs deletes the documents of indexName indexed from any of
// fileIDs. The index is refreshed first, and a document another file rewrote
// since its match is left alone (conflicts=proceed).
func (r *ElasticSearchRepository) DeleteByFileIDs(ctx context.Context, indexName string, fileIDs []string) error {
	if len(fileIDs) == 0 {
		return nil
	}
	body, err := json.Marshal(map[string]any{
		"query": map[string]any{"terms": map[string]any{"file_id": fileIDs}},
	})
	if err != nil {
		return err
	}

	refreshRes, err := r.esClient.Indices.Refresh(
		r.esClient.Indices.Refresh.WithContext(ctx),
		r.esClient.Indices.Refresh.WithIndex(indexName),
	)
	if err != nil {
		return fmt.Errorf("failed to refresh %q: %w", indexName, err)
	}
	defer func() { _ = refreshRes.Body.Close() }()
	if refreshRes.IsError() {
		return fmt.Errorf("elasticsearch refresh failed: %s", refreshRes.String())
	}

	res, err := r.esClient.DeleteByQuery(
		[]string{indexName},
		bytes.NewReader(body),
		r.esClient.DeleteByQuery.WithContext(ctx),
		r.esClient.DeleteByQuery.WithConflicts("proceed"),
	)
	if err != nil {
		return fmt.Errorf("failed to delete by file_ids: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.IsError() {
		return fmt.Errorf("elasticsearch delete_by_query failed: %s", res.String())
	}
	return nil
}

// SetAlias atomically points aliasName to indexName, removing only this
// assembly's previous index (if any) from the alias — sibling assemblies'
// indexes already attached to aliasName are left untouched.
func (r *ElasticSearchRepository) SetAlias(ctx context.Context, indexName, aliasName, assemblyKey string) error {
	actions := []map[string]any{}
	assemblyPrefix := aliasName + "-" + assemblyKey + "-"

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
			if !strings.HasPrefix(index, assemblyPrefix) {
				continue
			}
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
// A non-empty species keeps only that species' genes. It filters in the query, so
// other species' matches can't take the result size from this species' ones.
func (r *ElasticSearchRepository) FindBySynonymRelaxed(ctx context.Context, indexName, query, species string) ([]entity.Synonym, error) {
	q := map[string]any{"match": map[string]any{"synonym": query}}
	if species != "" {
		q = map[string]any{"bool": map[string]any{
			"must":   q,
			"filter": map[string]any{"prefix": map[string]any{"gene": species + ":"}},
		}}
	}
	body, err := json.Marshal(map[string]any{
		"query": q,
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
// A non-empty species suggests only that species' synonyms, from
// synonym.suggest_species; without one, every species' from synonym.suggest.
func (r *ElasticSearchRepository) Suggest(ctx context.Context, indexName, prefix, species string) ([]string, error) {
	field, contextName := "synonym.suggest", "synonym_type"
	if species != "" {
		field, contextName = "synonym.suggest_species", "species_type"
	}
	contexts := make([]map[string]any, len(suggestTypeBoosts))
	for i, t := range suggestTypeBoosts {
		context := t.synonymType
		if species != "" {
			context = speciesType(species, t.synonymType)
		}
		contexts[i] = map[string]any{"context": context, "boost": t.boost}
	}
	body, err := json.Marshal(map[string]any{
		"size": 0,
		"suggest": map[string]any{
			"suggest": map[string]any{
				"prefix": prefix,
				"completion": map[string]any{
					"field":           field,
					"size":            100,
					"skip_duplicates": true,
					"contexts":        map[string]any{contextName: contexts},
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
				Index  string         `json:"_index"`
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
		synonyms[i].Index = h.Index
	}
	return synonyms, nil
}
