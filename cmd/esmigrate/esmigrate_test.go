package esmigrate

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/EMOBase/emobase-genomics/internal/pkg/repository/estest"
)

// es:migrate runs on every deploy, over synonym indexes created before
// species_type existed. It must:
//   - give new indexes (template) and existing ones (put mapping) the same
//     fields, since ES rejects a put that changes an existing field;
//   - put every index's mapping before filling any, so a species-filtered
//     suggestion never meets an index without synonym.suggest_species;
//   - fill only documents without species_type, so a rerun changes nothing.
func TestRun_AddsSpeciesTypeToEveryExistingSynonymIndex(t *testing.T) {
	indexes := []string{"p-synonym-v1-v1a1-1700000000", "p-synonym-v1-v1a2-1700000000"}
	client, rt := estest.New(t, func(r estest.Request) string {
		if r.Method == "GET" && r.Path == "/p-synonym-*" {
			return estest.IndexList(indexes[1], indexes[0])
		}
		return ""
	})
	if err := run(context.Background(), client, "p"); err != nil {
		t.Fatal(err)
	}

	var templateProps, mappingProps any
	var steps []string
	for _, r := range rt.Requests {
		if r.Body != "" && !json.Valid([]byte(r.Body)) {
			t.Fatalf("%s %s: invalid JSON body %s", r.Method, r.Path, r.Body)
		}
		switch {
		case r.Path == "/_index_template/p-synonym":
			var tmpl struct {
				Template struct {
					Mappings struct{ Properties any } `json:"mappings"`
				} `json:"template"`
			}
			_ = json.Unmarshal([]byte(r.Body), &tmpl)
			templateProps = tmpl.Template.Mappings.Properties
		case strings.HasSuffix(r.Path, "/_mapping"):
			var m struct{ Properties any }
			_ = json.Unmarshal([]byte(r.Body), &m)
			mappingProps = m.Properties
			steps = append(steps, "mapping "+strings.TrimSuffix(strings.TrimPrefix(r.Path, "/"), "/_mapping"))
		case strings.HasSuffix(r.Path, "/_update_by_query"):
			if !strings.Contains(r.Body, `"must_not":{"exists":{"field":"species_type"}}`) || !strings.Contains(r.Query, "conflicts=proceed") {
				t.Errorf("update_by_query %s?%s %s: want only documents without species_type, conflicts=proceed", r.Path, r.Query, r.Body)
			}
			steps = append(steps, "fill "+strings.TrimSuffix(strings.TrimPrefix(r.Path, "/"), "/_update_by_query"))
		}
	}

	if templateProps == nil || !reflect.DeepEqual(templateProps, mappingProps) {
		t.Errorf("template properties %v\n differ from put mapping properties %v", templateProps, mappingProps)
	}
	want := []string{"mapping " + indexes[0], "mapping " + indexes[1], "fill " + indexes[0], "fill " + indexes[1]}
	if !reflect.DeepEqual(steps, want) {
		t.Errorf("steps %v, want %v (every mapping before any fill)", steps, want)
	}
}
