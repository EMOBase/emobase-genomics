package sequence

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/EMOBase/emobase-genomics/internal/pkg/repository/estest"
)

// One sequence index holds all of an assembly's sequence types, so deleting a
// replaced RNA file's sequences must be limited to TRANSCRIPT documents from a
// replaced file, or untagged ones indexed before documents carried their file.
// The index is refreshed first and conflicting deletes are skipped, so a
// document the new file just rewrote is never deleted under its old version.
func TestDeleteReplaced_ScopedToTypeAndReplacedFiles(t *testing.T) {
	client, rt := estest.New(t, nil)
	if err := New(client, 10).DeleteReplaced(context.Background(), "idx", "TRANSCRIPT", []string{"old-1"}); err != nil {
		t.Fatal(err)
	}

	if len(rt.Requests) != 2 || rt.Requests[0].Path != "/idx/_refresh" || rt.Requests[1].Path != "/idx/_delete_by_query" {
		t.Fatalf("requests %+v, want a refresh of idx, then a delete_by_query on idx", rt.Requests)
	}
	if !strings.Contains(rt.Requests[1].Query, "conflicts=proceed") {
		t.Errorf("delete_by_query params %q, want conflicts=proceed", rt.Requests[1].Query)
	}

	var q struct {
		Query struct {
			Bool struct {
				Filter []map[string]map[string]string `json:"filter"`
				Should []json.RawMessage              `json:"should"`
				Min    int                            `json:"minimum_should_match"`
			} `json:"bool"`
		} `json:"query"`
	}
	if err := json.Unmarshal([]byte(rt.Requests[1].Body), &q); err != nil {
		t.Fatalf("body %s: %v", rt.Requests[1].Body, err)
	}
	b := q.Query.Bool
	if len(b.Filter) != 1 || b.Filter[0]["term"]["type"] != "TRANSCRIPT" {
		t.Errorf("filter %v, want only type TRANSCRIPT", b.Filter)
	}
	should := string(b.Should[0]) + string(b.Should[1])
	if len(b.Should) != 2 || b.Min != 1 ||
		!strings.Contains(should, `"terms":{"file_id":["old-1"]}`) ||
		!strings.Contains(should, `"must_not":{"exists":{"field":"file_id"}}`) {
		t.Errorf("should %s (min %d), want file_id in [old-1] or no file_id, at least one", should, b.Min)
	}
}
