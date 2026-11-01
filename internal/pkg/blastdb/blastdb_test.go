package blastdb

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// The BLAST server serves every database in its folder, so removing an
// assembly or a Version must catch all of its files (every type, every
// makeblastdb extension) and nothing of another assembly or Version whose ID
// merely starts the same way.
func TestRemove_OnlyThatAssemblyOrVersion(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{
		"v1a7-genome.nsq", "v1a7-genome.nin", "v1a7-protein.psq",
		"v1a70-genome.nsq", "v1a8-genome.nsq", "v12a7-genome.nsq", "v2a1-rna.nsq",
	} {
		if err := os.WriteFile(filepath.Join(dir, n), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	left := func() []string {
		entries, _ := os.ReadDir(dir)
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		return names
	}

	if n, err := RemoveAssembly(dir, "v1a7"); err != nil || n != 3 {
		t.Fatalf("RemoveAssembly(v1a7) = %d, %v; want 3 files", n, err)
	}
	if want := []string{"v12a7-genome.nsq", "v1a70-genome.nsq", "v1a8-genome.nsq", "v2a1-rna.nsq"}; !slices.Equal(left(), want) {
		t.Fatalf("after removing v1a7: %v, want %v", left(), want)
	}

	if n, err := RemoveVersion(dir, 1); err != nil || n != 2 {
		t.Fatalf("RemoveVersion(1) = %d, %v; want 2 files", n, err)
	}
	if want := []string{"v12a7-genome.nsq", "v2a1-rna.nsq"}; !slices.Equal(left(), want) {
		t.Errorf("after removing version 1: %v, want %v", left(), want)
	}
}
