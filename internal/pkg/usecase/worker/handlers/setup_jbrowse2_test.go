package handlers

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	"github.com/EMOBase/emobase-genomics/internal/pkg/jobpayload"
)

// Each assembly links its genes to its own site through its info_url, used as
// entered: the script appends the gene ID to it, so an admin's trailing "/" or
// "?id=" must survive. Characters the script cannot embed are percent-encoded,
// since a quote would end the JEXL string the link is written into and break
// the detail panel of every gene in the track. An already-encoded URL is not
// encoded twice.
func TestGeneLinkBase(t *testing.T) {
	str := func(s string) *string { return &s }
	cases := []struct {
		name    string
		infoURL *string
		want    string
	}{
		{"no info_url: no link", nil, ""},
		{"path prefix kept", str("https://ibeetle-base.uni-goettingen.de/details/"), "https://ibeetle-base.uni-goettingen.de/details/"},
		{"query prefix kept", str("https://example.org/gene?id="), "https://example.org/gene?id="},
		{"already encoded kept", str("https://example.org/a%20b/"), "https://example.org/a%20b/"},
		{"unsafe characters encoded", str(`https://example.org/it's "x" <y> a\b /`), "https://example.org/it%27s%20%22x%22%20%3Cy%3E%20a%5Cb%20/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := geneLinkBase(tc.infoURL); got != tc.want {
				t.Errorf("geneLinkBase = %q, want %q", got, tc.want)
			}
		})
	}
}

type fakeLinkAssemblies struct{ IAssemblyVersionRepository }

func (fakeLinkAssemblies) FindByID(context.Context, uint64) (*entity.AssemblyVersion, error) {
	return nil, nil
}

// The link base is read from the assembly when the job runs. If the assembly
// is gone, the job fails before touching JBrowse2, rather than adding the
// annotation track with no gene links.
func TestSetupGFFJBrowse2_FailsWhenAssemblyIsGone(t *testing.T) {
	raw, err := json.Marshal(jobpayload.SetupJBrowse2GFFPayload{VersionID: 1, AssemblyVersionID: 7})
	if err != nil {
		t.Fatal(err)
	}
	p := json.RawMessage(raw)

	_, err = NewSetupGFFJBrowse2Handler(fakeLinkAssemblies{}).Handle(context.Background(), entity.Job{Payload: &p})
	if err == nil || !strings.Contains(err.Error(), "assembly 7 not found") {
		t.Fatalf("error = %v, want assembly 7 not found", err)
	}
}
