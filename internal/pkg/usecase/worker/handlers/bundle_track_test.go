package handlers

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	"github.com/EMOBase/emobase-genomics/internal/pkg/jobpayload"
)

const trackManifest = "fileName,trackName,category,selectInDefaultSession\n" +
	"t1.gff,Pre-blastoderm,RNA-Seq,true\n" +
	"t2.gff.gz,Second,,false\n"

func trackBundle(t *testing.T, manifest string, assemblyID uint64) (*BundleHandler, *fakeBundleFiles, *fakeBundleJobs, entity.Job, string) {
	t.Helper()
	asm := assemblyID
	return newBundleFixture(t, bundleCase{
		fileType:          entity.FileTypeJBrowseTrackBundle,
		assemblyVersionID: &asm,
		archiveDir:        filepath.Join("v1", "Tcas"),
		entries: []tarEntry{
			{name: "manifest.csv", body: manifest},
			{name: "t1.gff", body: "##gff-version 3"},
			{name: "t2.gff.gz", body: gzipped(t, "##gff-version 3")},
		},
	})
}

func TestBundleHandler_TrackChildrenBelongToBundleAssembly(t *testing.T) {
	h, files, jobs, job, root := trackBundle(t, trackManifest, 7)
	if _, err := h.Handle(context.Background(), job); err != nil {
		t.Fatal(err)
	}

	if len(files.created) != 2 {
		t.Fatalf("created %d child files, want 2", len(files.created))
	}
	// Tracks are per assembly, so their files live in the species folder, next to
	// the archive, and DeleteAssemblyVersion removes that folder with the assembly.
	wantDir := filepath.Join(root, "v1", "Tcas", testBundleID)
	for _, f := range files.created {
		if f.FileType != entity.FileTypeJBrowseTrack {
			t.Errorf("child %s type %q, want %q", f.ID, f.FileType, entity.FileTypeJBrowseTrack)
		}
		if f.AssemblyVersionID == nil || *f.AssemblyVersionID != 7 {
			t.Errorf("child %s assembly %v, want 7", f.ID, f.AssemblyVersionID)
		}
		if filepath.Dir(f.FilePath) != wantDir {
			t.Errorf("child %s stored in %s, want %s", f.ID, filepath.Dir(f.FilePath), wantDir)
		}
	}

	if len(jobs.created) != 2 {
		t.Fatalf("created %d child jobs, want 2", len(jobs.created))
	}
	for _, j := range jobs.created {
		if j.Type != entity.JobTypeJBrowseTrack {
			t.Errorf("job type %q, want %q", j.Type, entity.JobTypeJBrowseTrack)
		}
		if j.AssemblyVersionID == nil || *j.AssemblyVersionID != 7 {
			t.Errorf("job %d assembly %v, want 7", j.ID, j.AssemblyVersionID)
		}
		var p jobpayload.JBrowseTrackPayload
		if err := json.Unmarshal(*j.Payload, &p); err != nil {
			t.Fatal(err)
		}
		if p.VersionID != 1 || p.AssemblyVersionID != 7 {
			t.Errorf("job %d payload ids = (%d, %d), want (1, 7)", j.ID, p.VersionID, p.AssemblyVersionID)
		}
	}
	if got := readGzip(t, files.created[1].FilePath); got != "##gff-version 3" {
		t.Errorf("gzipped track content %q", got)
	}
}

// Two rows naming the same track in one bundle would produce two tracks with one
// name on the same assembly. The job must fail before anything is written.
func TestBundleHandler_DuplicateTrackNameFailsWholeBundle(t *testing.T) {
	dup := "fileName,trackName,category,selectInDefaultSession\n" +
		"t1.gff,Same,,false\n" +
		"t2.gff.gz,Same,,false\n"
	h, files, jobs, job, root := trackBundle(t, dup, 7)

	if _, err := h.Handle(context.Background(), job); err == nil {
		t.Fatal("duplicate trackName must fail the job")
	}
	if len(files.created) != 0 || len(jobs.created) != 0 {
		t.Errorf("created %d files and %d jobs, want none", len(files.created), len(jobs.created))
	}
	if _, err := os.Stat(filepath.Join(root, "v1", "Tcas", testBundleID)); !os.IsNotExist(err) {
		t.Error("no bundle folder may be left behind when validation fails")
	}
}

func TestBundleHandler_TrackRerunCreatesNoDuplicates(t *testing.T) {
	h, files, jobs, job, _ := trackBundle(t, trackManifest, 7)
	ctx := context.Background()
	if _, err := h.Handle(ctx, job); err != nil {
		t.Fatal(err)
	}
	firstFiles, firstJobs := len(files.created), len(jobs.created)

	if _, err := h.Handle(ctx, job); err != nil {
		t.Fatal(err)
	}
	if len(files.created) != firstFiles || len(jobs.created) != firstJobs {
		t.Errorf("rerun created %d files and %d jobs more, want none",
			len(files.created)-firstFiles, len(jobs.created)-firstJobs)
	}
}
