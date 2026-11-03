package upload

import (
	"errors"
	"testing"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

// The manifest template is served only for accepted bundle types. A
// non-bundle type has no manifest, so asking for one must be refused.
func TestManifestColumns_OnlyAcceptedBundleTypes(t *testing.T) {
	uc := &UseCase{}

	for _, ft := range []string{entity.FileTypeOrthologyBundle, entity.FileTypeJBrowseTrackBundle} {
		cols, err := uc.ManifestColumns(ft)
		if err != nil {
			t.Fatalf("%s: %v", ft, err)
		}
		if len(cols) == 0 || cols[0] != "fileName" {
			t.Errorf("%s columns = %v", ft, cols)
		}
	}

	if _, err := uc.ManifestColumns(entity.FileTypeGenomicGFF); !errors.Is(err, ErrUnknownBundleType) {
		t.Errorf("genomic.gff: got %v, want ErrUnknownBundleType", err)
	}
}

// Version-scoped types take no "assembly" metadata: orthology belongs to the
// whole version, not to one species. Everything else must name its assembly.
func TestIsVersionScoped(t *testing.T) {
	for _, ft := range []string{entity.FileTypeOrthologyTSV, entity.FileTypeOrthologyBundle} {
		if !isVersionScoped(ft) {
			t.Errorf("%s should be version-scoped", ft)
		}
	}
	for _, ft := range []string{entity.FileTypeGenomicFNA, entity.FileTypeGenomicGFF, entity.FileTypeJBrowseTrack} {
		if isVersionScoped(ft) {
			t.Errorf("%s should require an assembly", ft)
		}
	}
}
