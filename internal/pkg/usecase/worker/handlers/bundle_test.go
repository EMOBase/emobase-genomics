package handlers

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	"github.com/EMOBase/emobase-genomics/internal/pkg/uploadspec"
)

type tarEntry struct {
	name     string
	body     string
	typeflag byte // defaults to tar.TypeReg
}

func writeBundle(t *testing.T, entries ...tarEntry) string {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Mode: 0644, Size: int64(len(e.body)), Typeflag: e.typeflag}
		if e.typeflag == 0 {
			hdr.Typeflag = tar.TypeReg
		}
		if hdr.Typeflag != tar.TypeReg {
			hdr.Size = 0
			hdr.Linkname = "target"
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if hdr.Typeflag == tar.TypeReg {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "bundle.tar.gz")
	if err := os.WriteFile(p, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func gzipped(t *testing.T, s string) string {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	_, _ = gw.Write([]byte(s))
	_ = gw.Close()
	return buf.String()
}

func readGzip(t *testing.T, p string) string {
	t.Helper()
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("%s is not a valid gzip file: %v", p, err)
	}
	b, err := io.ReadAll(gr)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func requireErrContains(t *testing.T, err error, wants ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an error containing %q, got nil", wants)
	}
	for _, w := range wants {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("error %q does not contain %q", err.Error(), w)
		}
	}
}

// An entry that could resolve outside the bundle directory must be rejected
// before anything is written: the worker writes into the shared uploads volume.
func TestScanBundle_RejectsEntriesThatEscapeTheBundleDir(t *testing.T) {
	cases := map[string]tarEntry{
		"parent traversal": {name: "../evil.tsv", body: "x"},
		"absolute path":    {name: "/etc/evil.tsv", body: "x"},
		"nested folder":    {name: "dir/a.tsv", body: "x"},
		"symlink":          {name: "link.tsv", typeflag: tar.TypeSymlink},
		"hard link":        {name: "hard.tsv", typeflag: tar.TypeLink},
		"folder entry":     {name: "dir/", typeflag: tar.TypeDir},
	}
	for name, entry := range cases {
		t.Run(name, func(t *testing.T) {
			p := writeBundle(t, tarEntry{name: "manifest.csv", body: "fileName,order,algorithm\n"}, entry)
			_, _, err := scanBundle(p)
			requireErrContains(t, err, entry.name)
		})
	}
}

// `tar czf b.tar.gz -C <folder> .` is the documented way to build a bundle; its
// "./" root entry and "./" name prefixes must not be mistaken for a folder.
func TestScanBundle_AcceptsDotSlashArchives(t *testing.T) {
	p := writeBundle(t,
		tarEntry{name: "./", typeflag: tar.TypeDir},
		tarEntry{name: "./manifest.csv", body: "fileName,order,algorithm\n"},
		tarEntry{name: "./a.tsv", body: "x"},
	)
	entries, manifest, err := scanBundle(p)
	if err != nil {
		t.Fatal(err)
	}
	if !entries["a.tsv"] || len(entries) != 1 || manifest == nil {
		t.Fatalf("unexpected scan result: entries=%v manifest=%q", entries, manifest)
	}
}

// Without this check, "a.tsv" (gzipped on extraction) and "a.tsv.gz" would be
// written to the same path and one child's data would silently replace the other's.
func TestScanBundle_RejectsStoredNameCollision(t *testing.T) {
	p := writeBundle(t,
		tarEntry{name: "manifest.csv", body: "fileName,order,algorithm\n"},
		tarEntry{name: "a.tsv", body: "x"},
		tarEntry{name: "a.tsv.gz", body: gzipped(t, "y")},
	)
	_, _, err := scanBundle(p)
	requireErrContains(t, err, "would both be stored as")
}

func TestScanBundle_RequiresManifest(t *testing.T) {
	p := writeBundle(t, tarEntry{name: "a.tsv", body: "x"})
	_, _, err := scanBundle(p)
	requireErrContains(t, err, "no manifest.csv")
}

// A truncated upload must fail validation, not extract partial files that
// child jobs would then index as if they were complete.
func TestScanBundle_RejectsTruncatedArchive(t *testing.T) {
	p := writeBundle(t,
		tarEntry{name: "manifest.csv", body: "fileName,order,algorithm\na.tsv,1,X\n"},
		tarEntry{name: "a.tsv", body: strings.Repeat("gene\tgene\n", 5000)},
	)
	data, _ := os.ReadFile(p)
	if err := os.WriteFile(p, data[:len(data)-20], 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := scanBundle(p); err == nil {
		t.Fatal("expected truncated archive to be rejected")
	}
}

// Validation is all-or-nothing and must report every problem at once, so the
// user can fix the manifest in one round trip instead of one error per upload.
func TestParseManifest_ReportsAllProblems(t *testing.T) {
	spec := uploadspec.Bundles[entity.FileTypeOrthologyBundle]
	entries := map[string]bool{"a.tsv": true, "b.tsv": true, "unlisted.tsv": true}
	manifest := "fileName,order,algorithm\n" +
		"a.tsv,notanumber,X\n" +
		"b.tsv,2,\n" +
		"a.tsv,3,X\n" +
		"missing.tsv,4,X\n"

	_, err := parseManifest([]byte(manifest), spec, entries)
	requireErrContains(t, err,
		`line 2 (a.tsv): orthology.tsv "order" metadata field must be an integer`,
		`line 3 (b.tsv): orthology.tsv uploads require an "algorithm" metadata field`,
		`line 4 (a.tsv): fileName is listed more than once`,
		`line 5 (missing.tsv): file not found in the archive`,
		`"unlisted.tsv" is in the archive but not listed`,
	)
}

// Columns from another bundle type (or typos) must be rejected rather than
// silently ignored, otherwise metadata the user filled in would be dropped.
func TestParseManifest_HeaderMustMatchTemplate(t *testing.T) {
	spec := uploadspec.Bundles[entity.FileTypeOrthologyBundle]
	_, err := parseManifest([]byte("fileName,order,trackName\na.tsv,1,T\n"), spec, map[string]bool{"a.tsv": true})
	requireErrContains(t, err, `unknown column "trackName"`, `missing column "algorithm"`)
}

// Spreadsheet exports reorder columns, add a BOM and trailing blank rows; none
// of that should make a correct manifest fail.
func TestParseManifest_ToleratesSpreadsheetExports(t *testing.T) {
	spec := uploadspec.Bundles[entity.FileTypeOrthologyBundle]
	manifest := "\xEF\xBB\xBFalgorithm, order ,fileName\r\nOrthoFinder,1, a.tsv \r\nOrthoFinder,1,b.tsv\r\n,,\r\n"
	rows, err := parseManifest([]byte(manifest), spec, map[string]bool{"a.tsv": true, "b.tsv": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0]["fileName"] != "a.tsv" || rows[0]["order"] != "1" || rows[0]["algorithm"] != "OrthoFinder" {
		t.Fatalf("unexpected rows: %v", rows)
	}
}

// Two tracks with the same display name in one bundle are indistinguishable in
// JBrowse2's track selector — almost certainly a copy-paste mistake.
func TestParseManifest_RejectsDuplicateTrackName(t *testing.T) {
	spec := uploadspec.Bundles[entity.FileTypeJBrowseTrackBundle]
	manifest := "fileName,trackName,category,selectInDefaultSession\n" +
		"a.gff,RNA-seq,,\n" +
		"b.gff,RNA-seq,,\n"
	_, err := parseManifest([]byte(manifest), spec, map[string]bool{"a.gff": true, "b.gff": true})
	requireErrContains(t, err, `trackName "RNA-seq" is already used by a.gff`)
}

// Child handlers read input through gzip.NewReader: plain files must be
// gzipped, and already-gzipped files must not be gzipped a second time
// (the handler would then read compressed bytes as text).
func TestExtractBundle_GzipsPlainFilesOnce(t *testing.T) {
	p := writeBundle(t,
		tarEntry{name: "manifest.csv", body: "ignored"},
		tarEntry{name: "plain.tsv", body: "plain-content"},
		tarEntry{name: "pre.tsv.gz", body: gzipped(t, "pre-content")},
		tarEntry{name: "pre-no-suffix.tsv", body: gzipped(t, "pre-no-suffix-content")},
	)
	dst := filepath.Join(t.TempDir(), "bundle-id")
	if err := extractBundle(p, dst); err != nil {
		t.Fatal(err)
	}

	for file, want := range map[string]string{
		"plain.tsv.gz":         "plain-content",
		"pre.tsv.gz":           "pre-content",
		"pre-no-suffix.tsv.gz": "pre-no-suffix-content",
	} {
		if got := readGzip(t, filepath.Join(dst, file)); got != want {
			t.Errorf("%s: got %q, want %q", file, got, want)
		}
	}
	if _, err := os.Stat(filepath.Join(dst, "manifest.csv.gz")); !os.IsNotExist(err) {
		t.Error("manifest.csv must not be extracted as a data file")
	}
}

// A rerun after a crash must find the children it already created rather than
// enqueue every file a second time.
func TestBundleChildID_DeterministicAndUnique(t *testing.T) {
	a := bundleChildID("bundle1", "a.tsv")
	if a != bundleChildID("bundle1", "a.tsv") {
		t.Fatal("child ID must be stable across reruns")
	}
	if a == bundleChildID("bundle1", "b.tsv") || a == bundleChildID("bundle2", "a.tsv") {
		t.Fatal("child IDs must differ across files and bundles")
	}
	if len(a) > 36 { // upload_files.id / jobs.file_id are VARCHAR(36)
		t.Fatalf("child ID %q is longer than 36 characters", a)
	}
}
