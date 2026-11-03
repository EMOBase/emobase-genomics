package uploadspec

import "testing"

// The tag must keep everything handlers and scripts read from a stored name:
// the full extension chain (format detection: .zip, .gz, .gff, ...) and the
// start of the name (the synonym parser is picked by prefix). And it must come
// from the upload ID, so two uploads of one name never share a path.
func TestStoredFileName(t *testing.T) {
	const id = "1a2b3c4d5e6f708192a3b4c5d6e7f809" // tusd ID: 32 hex characters
	for name, want := range map[string]string{
		"genomic.gff.gz":               "genomic-1a2b3c4d.gff.gz",
		"track_bundle.zip":             "track_bundle-1a2b3c4d.zip",
		"fb_synonym_fb_2024_01.tsv.gz": "fb_synonym_fb_2024_01-1a2b3c4d.tsv.gz",
		"au4.liftover.sorted.gff.gz":   "au4-1a2b3c4d.liftover.sorted.gff.gz",
		"README":                       "README-1a2b3c4d",
		".hidden.gz":                   ".hidden-1a2b3c4d.gz",
	} {
		if got := StoredFileName(name, id); got != want {
			t.Errorf("StoredFileName(%q) = %q, want %q", name, got, want)
		}
	}

	// Bundle child IDs are dashed UUIDs; the tag skips the dashes.
	if got := StoredFileName("ib.gff.gz", "9f86d081-884c-5d63-a1b2-c3d4e5f60718"); got != "ib-9f86d081.gff.gz" {
		t.Errorf("dashed ID: got %q, want %q", got, "ib-9f86d081.gff.gz")
	}

	if StoredFileName("genomic.gff.gz", "aaaaaaaa11111111") == StoredFileName("genomic.gff.gz", "bbbbbbbb22222222") {
		t.Error("two uploads of one name must get different stored names")
	}
}
