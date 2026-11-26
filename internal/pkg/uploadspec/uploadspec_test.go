package uploadspec

import (
	"encoding/json"
	"testing"

	"github.com/EMOBase/emobase-genomics/internal/pkg/jobpayload"
)

// selectInDefaultSession decides whether a track is shown the first time
// someone opens its assembly in JBrowse2. A value the server cannot read must be
// rejected, not read as false: the upload would otherwise succeed with the track
// left out of the default session, and nothing would say why. Spreadsheet
// values (TRUE) and stray spaces are still read, since they mean what they say.
func TestSelectInDefaultSession(t *testing.T) {
	cases := []struct {
		value   string
		want    bool
		invalid bool
	}{
		{value: "", want: false},
		{value: "true", want: true},
		{value: "false", want: false},
		{value: "TRUE", want: true},
		{value: "False", want: false},
		{value: " true ", want: true},
		{value: "yes", invalid: true},
		{value: "on", invalid: true},
		{value: "x", invalid: true},
	}
	asm := uint64(7)
	for _, tc := range cases {
		t.Run(tc.value, func(t *testing.T) {
			meta := map[string]string{"trackName": "Coverage", "selectInDefaultSession": tc.value}

			err := ValidateJBrowseTrackMeta(meta)
			if tc.invalid != (err != nil) {
				t.Fatalf("ValidateJBrowseTrackMeta(%q) error = %v, want invalid=%v", tc.value, err, tc.invalid)
			}

			job, err := NewJBrowseTrackJob(1, &asm, "f1", "/uploads/f1.bw.gz", meta)
			if tc.invalid {
				if err == nil {
					t.Fatalf("NewJBrowseTrackJob(%q) built a job, want an error", tc.value)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var p jobpayload.JBrowseTrackPayload
			if err := json.Unmarshal(*job.Payload, &p); err != nil {
				t.Fatal(err)
			}
			if p.SelectInDefaultSession != tc.want {
				t.Errorf("selectInDefaultSession %q read as %v, want %v", tc.value, p.SelectInDefaultSession, tc.want)
			}
		})
	}
}
