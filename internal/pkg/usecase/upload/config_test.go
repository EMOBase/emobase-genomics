package upload

import (
	"errors"
	"testing"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

// The manifest template is only served for bundle types that are wired end to
// end. The track bundle has no assembly handling yet, so its template must not
// be published before it is accepted.
func TestManifestColumns_OnlyAcceptedBundleTypes(t *testing.T) {
	uc := &UseCase{}

	cols, err := uc.ManifestColumns(entity.FileTypeOrthologyBundle)
	if err != nil {
		t.Fatalf("orthology.bundle: %v", err)
	}
	if len(cols) != 3 || cols[0] != "fileName" {
		t.Errorf("orthology.bundle columns = %v", cols)
	}

	if _, err := uc.ManifestColumns(entity.FileTypeJBrowseTrackBundle); !errors.Is(err, ErrUnknownBundleType) {
		t.Errorf("jbrowse.track.bundle: got %v, want ErrUnknownBundleType", err)
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
