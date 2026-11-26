package handlers

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

func orthologyBundle(t *testing.T) (*BundleHandler, *fakeBundleFiles, *fakeBundleJobs, entity.Job) {
	t.Helper()
	h, files, jobs, job, _ := newBundleFixture(t, bundleCase{
		fileType:   entity.FileTypeOrthologyBundle,
		archiveDir: "v1",
		entries: []tarEntry{
			{name: "manifest.csv", body: "fileName,order,algorithm\na.tsv,1,OrthoFinder\nb.tsv.gz,2,Eggnog\n"},
			{name: "a.tsv", body: "group\tHsap\tMmus\nog1\tHsap:g1\tMmus:g1"},
			{name: "b.tsv.gz", body: gzipped(t, "group\tHsap\tMmus\nog2\tHsap:g2\tMmus:g2")},
		},
	})
	return h, files, jobs, job
}

func TestBundleHandler_OrthologyChildrenCarryNoAssembly(t *testing.T) {
	h, files, jobs, job := orthologyBundle(t)
	if _, err := h.Handle(context.Background(), job); err != nil {
		t.Fatal(err)
	}

	// Orthology belongs to the whole version. A child with an assembly would
	// drop out of the shared orthology status and release gate.
	if len(files.created) != 2 {
		t.Fatalf("created %d child files, want 2", len(files.created))
	}
	for _, f := range files.created {
		if f.FileType != entity.FileTypeOrthologyTSV {
			t.Errorf("child %s type %q, want %q", f.ID, f.FileType, entity.FileTypeOrthologyTSV)
		}
		if f.AssemblyVersionID != nil {
			t.Errorf("child %s has assembly %d, want none", f.ID, *f.AssemblyVersionID)
		}
		if f.CreatedBy != "alice" {
			t.Errorf("child %s created_by %q, want the bundle uploader", f.ID, f.CreatedBy)
		}
	}

	if len(jobs.created) != 2 {
		t.Fatalf("created %d child jobs, want 2", len(jobs.created))
	}
	for _, j := range jobs.created {
		if j.Type != entity.JobTypeOrthologyTSV {
			t.Errorf("job type %q, want %q", j.Type, entity.JobTypeOrthologyTSV)
		}
		if j.AssemblyVersionID != nil {
			t.Errorf("job %d has assembly %d, want none", j.ID, *j.AssemblyVersionID)
		}
	}

	if got := readGzip(t, files.created[0].FilePath); got != "group\tHsap\tMmus\nog1\tHsap:g1\tMmus:g1" {
		t.Errorf("child content %q", got)
	}
	if filepath.Dir(files.created[0].FilePath) != filepath.Dir(files.created[1].FilePath) {
		t.Error("children of one bundle must share the bundle's folder")
	}
}

// A requeued job (stuck-job recovery) must not duplicate children or jobs.
func TestBundleHandler_OrthologyRerunCreatesNoDuplicates(t *testing.T) {
	h, files, jobs, job := orthologyBundle(t)
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
