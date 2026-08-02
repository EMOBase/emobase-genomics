// Package esbulk decodes Elasticsearch _bulk API responses. All ES-backed
// repositories (genomic, sequence, orthology, synonym, dsrna) issue bulk
// index requests and previously only checked the top-level "errors" flag,
// which signals that *some* item failed but not why — the actual per-item
// error type/reason is only present on the failed items themselves.
package esbulk

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

const maxReasonLen = 300

type bulkResponse struct {
	Errors bool `json:"errors"`
	Items  []struct {
		Index struct {
			ID     string `json:"_id"`
			Status int    `json:"status"`
			Error  *struct {
				Type   string `json:"type"`
				Reason string `json:"reason"`
			} `json:"error"`
		} `json:"index"`
	} `json:"items"`
}

// DecodeResponse decodes a _bulk response body and returns nil if every item
// succeeded. If any item failed, it returns an error summarizing the distinct
// error type/reason combinations seen, how many items each affected, and an
// example document id, so failures like circuit breakers, mapping conflicts,
// or version conflicts are distinguishable from the log line alone.
func DecodeResponse(body io.Reader, indexName string) error {
	var result bulkResponse
	if err := json.NewDecoder(body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode bulk response: %w", err)
	}
	if !result.Errors {
		return nil
	}

	type key struct{ errType, reason string }
	counts := make(map[key]int)
	exampleID := ""
	failed := 0
	for _, item := range result.Items {
		if item.Index.Error == nil {
			continue
		}
		failed++
		if exampleID == "" {
			exampleID = item.Index.ID
		}
		reason := item.Index.Error.Reason
		if len(reason) > maxReasonLen {
			reason = reason[:maxReasonLen] + "..."
		}
		counts[key{item.Index.Error.Type, reason}]++
	}

	summaries := make([]string, 0, len(counts))
	for k, n := range counts {
		summaries = append(summaries, fmt.Sprintf("%s x%d: %s", k.errType, n, k.reason))
	}
	sort.Strings(summaries)

	return fmt.Errorf(
		"elasticsearch bulk index had %d/%d item failure(s) for index %q [%s] (example doc id: %s)",
		failed, len(result.Items), indexName, strings.Join(summaries, " | "), exampleID,
	)
}
