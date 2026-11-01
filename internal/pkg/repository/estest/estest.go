// Package estest fakes an Elasticsearch server for repository tests: it
// records every request and answers it with a 200.
package estest

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/elastic/go-elasticsearch/v8"
)

// Request is one request the client sent.
type Request struct{ Method, Path, Query, Body string }

// Server records requests in Requests and answers each with the body that
// respond returns ("{}" when respond is nil or returns "").
type Server struct {
	Requests []Request
	respond  func(Request) string
}

// New returns a client that talks to a new fake Server.
func New(t *testing.T, respond func(Request) string) (*elasticsearch.Client, *Server) {
	t.Helper()
	s := &Server{respond: respond}
	client, err := elasticsearch.NewClient(elasticsearch.Config{Transport: s, Addresses: []string{"http://es:9200"}})
	if err != nil {
		t.Fatal(err)
	}
	return client, s
}

func (s *Server) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		body, _ = io.ReadAll(req.Body)
	}
	r := Request{req.Method, req.URL.Path, req.URL.RawQuery, string(body)}
	s.Requests = append(s.Requests, r)
	out := "{}"
	if s.respond != nil {
		if b := s.respond(r); b != "" {
			out = b
		}
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"X-Elastic-Product": {"Elasticsearch"}, "Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(out)),
	}, nil
}

// IndexList answers an index GET with these index names.
func IndexList(names ...string) string {
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = `"` + n + `":{}`
	}
	return "{" + strings.Join(parts, ",") + "}"
}
