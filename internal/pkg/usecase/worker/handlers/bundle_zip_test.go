package handlers

import (
	"archive/tar"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

// A .zip track bundle is read like a .tar.gz one: same manifest, same children,
// same folder, and an already-gzipped member is kept as it is.
func TestBundleHandler_ZipTrackBundleExtractsLikeTar(t *testing.T) {
	asm := uint64(7)
	h, files, jobs, job, root := newBundleFixture(t, bundleCase{
		fileType:          entity.FileTypeJBrowseTrackBundle,
		assemblyVersionID: &asm,
		archiveDir:        filepath.Join("v1", "Tcas"),
		zip:               true,
		entries: []tarEntry{
			{name: "manifest.csv", body: trackManifest},
			{name: "t1.gff", body: "##gff-version 3"},
			{name: "t2.gff.gz", body: gzipped(t, "##gff-version 3")},
		},
	})
	if _, err := h.Handle(context.Background(), job); err != nil {
		t.Fatal(err)
	}

	if len(files.created) != 2 || len(jobs.created) != 2 {
		t.Fatalf("created %d files and %d jobs, want 2 each", len(files.created), len(jobs.created))
	}
	wantDir := filepath.Join(root, "v1", "Tcas", testBundleID)
	for _, f := range files.created {
		if f.AssemblyVersionID == nil || *f.AssemblyVersionID != 7 {
			t.Errorf("child %s assembly %v, want 7", f.ID, f.AssemblyVersionID)
		}
		if filepath.Dir(f.FilePath) != wantDir {
			t.Errorf("child %s stored in %s, want %s", f.ID, filepath.Dir(f.FilePath), wantDir)
		}
	}
	if got := readGzip(t, files.created[0].FilePath); got != "##gff-version 3" {
		t.Errorf("plain member gzipped as %q", got)
	}
	if got := readGzip(t, files.created[1].FilePath); got != "##gff-version 3" {
		t.Errorf("already-gzipped member read as %q", got)
	}
}

// Orthology bundles are version-scoped whether they arrive as .zip or .tar.gz.
func TestBundleHandler_ZipOrthologyChildrenCarryNoAssembly(t *testing.T) {
	h, files, jobs, job, _ := newBundleFixture(t, bundleCase{
		fileType:   entity.FileTypeOrthologyBundle,
		archiveDir: "v1",
		zip:        true,
		entries: []tarEntry{
			{name: "manifest.csv", body: "fileName,order,algorithm\na.tsv,1,OrthoFinder\n"},
			{name: "a.tsv", body: "group\tHsap\tMmus\nog1\tHsap:g1\tMmus:g1"},
		},
	})
	if _, err := h.Handle(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if len(files.created) != 1 || len(jobs.created) != 1 {
		t.Fatalf("created %d files and %d jobs, want 1 each", len(files.created), len(jobs.created))
	}
	if files.created[0].AssemblyVersionID != nil {
		t.Errorf("orthology child has assembly %d, want none", *files.created[0].AssemblyVersionID)
	}
}

// A folder inside a .zip is refused for the whole bundle, the same as in a
// .tar.gz: nothing is written, and no bundle folder is left behind.
func TestBundleHandler_ZipFolderEntryFailsWholeBundle(t *testing.T) {
	asm := uint64(7)
	h, files, jobs, job, root := newBundleFixture(t, bundleCase{
		fileType:          entity.FileTypeJBrowseTrackBundle,
		assemblyVersionID: &asm,
		archiveDir:        filepath.Join("v1", "Tcas"),
		zip:               true,
		entries: []tarEntry{
			{name: "manifest.csv", body: trackManifest},
			{name: "sub", typeflag: tar.TypeDir},
			{name: "t1.gff", body: "##gff-version 3"},
			{name: "t2.gff.gz", body: gzipped(t, "##gff-version 3")},
		},
	})
	if _, err := h.Handle(context.Background(), job); err == nil {
		t.Fatal("folder entry must fail the job")
	}
	if len(files.created) != 0 || len(jobs.created) != 0 {
		t.Errorf("created %d files and %d jobs, want none", len(files.created), len(jobs.created))
	}
	if _, err := os.Stat(filepath.Join(root, "v1", "Tcas", testBundleID)); !os.IsNotExist(err) {
		t.Error("no bundle folder may be left behind when validation fails")
	}
}

// A truncated .zip has no central directory, so it cannot be read at all. The
// job must fail before anything is written.
func TestBundleHandler_TruncatedZipFailsBeforeWriting(t *testing.T) {
	asm := uint64(7)
	h, files, jobs, job, root := newBundleFixture(t, bundleCase{
		fileType:          entity.FileTypeJBrowseTrackBundle,
		assemblyVersionID: &asm,
		archiveDir:        filepath.Join("v1", "Tcas"),
		zip:               true,
		entries: []tarEntry{
			{name: "manifest.csv", body: trackManifest},
			{name: "t1.gff", body: "##gff-version 3"},
			{name: "t2.gff.gz", body: gzipped(t, "##gff-version 3")},
		},
	})
	archive := filepath.Join(root, "v1", "Tcas", testBundleID+".zip")
	data, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archive, data[:len(data)/2], 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := h.Handle(context.Background(), job); err == nil {
		t.Fatal("truncated archive must fail the job")
	}
	if len(files.created) != 0 || len(jobs.created) != 0 {
		t.Errorf("created %d files and %d jobs, want none", len(files.created), len(jobs.created))
	}
	if _, err := os.Stat(filepath.Join(root, "v1", "Tcas", testBundleID)); !os.IsNotExist(err) {
		t.Error("no bundle folder may be left behind when validation fails")
	}
}
