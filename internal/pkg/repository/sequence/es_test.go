package sequence

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/elastic/go-elasticsearch/v8"
)

type recordedRequest struct{ method, path, query, body string }

// recordingTransport answers every request with an empty 200, as Elasticsearch.
type recordingTransport struct{ reqs []recordedRequest }

func (rt *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		body, _ = io.ReadAll(req.Body)
	}
	rt.reqs = append(rt.reqs, recordedRequest{req.Method, req.URL.Path, req.URL.RawQuery, string(body)})
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"X-Elastic-Product": {"Elasticsearch"}, "Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{}`)),
	}, nil
}

// One sequence index holds all of an assembly's sequence types, so deleting a
// replaced RNA file's sequences must be limited to TRANSCRIPT documents from a
// replaced file, or untagged ones indexed before documents carried their file.
// The index is refreshed first and conflicting deletes are skipped, so a
// document the new file just rewrote is never deleted under its old version.
func TestDeleteReplaced_ScopedToTypeAndReplacedFiles(t *testing.T) {
	rt := &recordingTransport{}
	client, err := elasticsearch.NewClient(elasticsearch.Config{Transport: rt, Addresses: []string{"http://es:9200"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := New(client, 10).DeleteReplaced(context.Background(), "idx", "TRANSCRIPT", []string{"old-1"}); err != nil {
		t.Fatal(err)
	}

	if len(rt.reqs) != 2 || rt.reqs[0].path != "/idx/_refresh" || rt.reqs[1].path != "/idx/_delete_by_query" {
		t.Fatalf("requests %+v, want a refresh of idx, then a delete_by_query on idx", rt.reqs)
	}
	if !strings.Contains(rt.reqs[1].query, "conflicts=proceed") {
		t.Errorf("delete_by_query params %q, want conflicts=proceed", rt.reqs[1].query)
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
	if err := json.Unmarshal([]byte(rt.reqs[1].body), &q); err != nil {
		t.Fatalf("body %s: %v", rt.reqs[1].body, err)
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
