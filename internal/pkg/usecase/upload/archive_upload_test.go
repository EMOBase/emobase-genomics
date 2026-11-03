package upload

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

// A bundle may arrive as a .zip, as well as the .tar.gz it has always taken.
func TestBundleUpload_AcceptsZipArchive(t *testing.T) {
	uc, _ := newTestUseCase()
	if _, err := preUpload(uc, trackBundleMeta(map[string]string{"assembly": "7", "fileName": "bundle.zip"})); err != nil {
		t.Fatalf("zip bundle refused: %v", err)
	}
}

// Only .tar.gz and .zip are archives. A bare .gz holds one file, not a bundle.
func TestBundleUpload_RejectsPlainGzipName(t *testing.T) {
	uc, _ := newTestUseCase()
	_, err := preUpload(uc, trackBundleMeta(map[string]string{"assembly": "7", "fileName": "bundle.gz"}))
	requireStatus(t, err, http.StatusBadRequest, ".tar.gz or .zip")
}

// Single files still take only gzip, so a .zip is refused for them.
func TestSingleUpload_RejectsZipName(t *testing.T) {
	uc, _ := newTestUseCase()
	meta := trackBundleMeta(map[string]string{"assembly": "7", "fileName": "t.zip"})
	meta["fileType"] = entity.FileTypeJBrowseTrack
	_, err := preUpload(uc, meta)
	requireStatus(t, err, http.StatusBadRequest, "only gzip files")
}

// The stored bytes must match the name: a .zip must start with the zip
// signature, and anything else with the gzip one.
func TestHasArchiveMagic(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, data []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	zipBytes := []byte{'P', 'K', 0x03, 0x04, 0, 0}
	gzipBytes := []byte{0x1f, 0x8b, 8, 0}

	cases := []struct {
		name string
		data []byte
		want bool
	}{
		{"bundle.zip", zipBytes, true},
		{"bundle.zip", gzipBytes, false},
		{"bundle.tar.gz", gzipBytes, true},
		{"bundle.tar.gz", zipBytes, false},
	}
	for i, c := range cases {
		got, err := hasArchiveMagic(write(fmt.Sprintf("case%d-%s", i, c.name), c.data), c.name)
		if err != nil {
			t.Fatal(err)
		}
		if got != c.want {
			t.Errorf("%s with %v: got %v, want %v", c.name, c.data[:2], got, c.want)
		}
	}
}
