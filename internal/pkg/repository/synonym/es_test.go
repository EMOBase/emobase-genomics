package synonym

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/elastic/go-elasticsearch/v8"
)

type recordedRequest struct{ path, query, body string }

// recordingTransport answers every request with an empty 200, as Elasticsearch.
type recordingTransport struct{ reqs []recordedRequest }

func (rt *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		body, _ = io.ReadAll(req.Body)
	}
	rt.reqs = append(rt.reqs, recordedRequest{req.URL.Path, req.URL.RawQuery, string(body)})
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"X-Elastic-Product": {"Elasticsearch"}, "Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{}`)),
	}, nil
}

// The synonyms of replaced GFF files are deleted by their file IDs only, from
// the index refreshed first, skipping conflicts so a synonym the new GFF just
// rewrote is never deleted under its old version. With no replaced file there
// is nothing to delete, and no request.
func TestDeleteByFileIDs_RefreshesThenDeletesThoseFilesOnly(t *testing.T) {
	rt := &recordingTransport{}
	client, err := elasticsearch.NewClient(elasticsearch.Config{Transport: rt, Addresses: []string{"http://es:9200"}})
	if err != nil {
		t.Fatal(err)
	}
	repo := New(client, 10)

	if err := repo.DeleteByFileIDs(context.Background(), "idx", nil); err != nil || len(rt.reqs) != 0 {
		t.Fatalf("no file IDs: err %v, requests %+v, want none", err, rt.reqs)
	}
	if err := repo.DeleteByFileIDs(context.Background(), "idx", []string{"gff-old"}); err != nil {
		t.Fatal(err)
	}
	if len(rt.reqs) != 2 || rt.reqs[0].path != "/idx/_refresh" || rt.reqs[1].path != "/idx/_delete_by_query" {
		t.Fatalf("requests %+v, want a refresh of idx, then a delete_by_query on idx", rt.reqs)
	}
	if !strings.Contains(rt.reqs[1].query, "conflicts=proceed") {
		t.Errorf("delete_by_query params %q, want conflicts=proceed", rt.reqs[1].query)
	}
	if want := `{"query":{"terms":{"file_id":["gff-old"]}}}`; strings.TrimSpace(rt.reqs[1].body) != want {
		t.Errorf("body %s, want %s", rt.reqs[1].body, want)
	}
}
