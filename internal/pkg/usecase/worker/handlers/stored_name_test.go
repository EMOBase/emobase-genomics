package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	"github.com/EMOBase/emobase-genomics/internal/pkg/jobpayload"
	"github.com/EMOBase/emobase-genomics/internal/pkg/uploadspec"
	synonymparser "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/synonym/parser"
)

// The synonym parser is picked from the stored file name, which now carries an
// upload ID tag. Every supported kind of file must still be recognized.
func TestSynonymParserForFile_RecognizesTaggedNames(t *testing.T) {
	const id = "1a2b3c4d5e6f708192a3b4c5d6e7f809"
	h := &SynonymHandler{}
	for name, want := range map[string]any{
		"genomic.gff.gz":                   &synonymparser.GFF3SynonymParser{},
		"annotation.v2.gff3.gz":            &synonymparser.GFF3SynonymParser{},
		"fb_synonym_fb_2024_01.tsv.gz":     &synonymparser.FlyBaseSynonymParser{},
		"fbgn_fbtr_fbpp_fb_2024_01.tsv.gz": &synonymparser.FlyBaseGeneRNAProteinMapParser{},
		"IB_TC_mapping.tsv.gz":             &synonymparser.IBTCParser{},
	} {
		path := filepath.Join("public/uploads/v1/v1a7", uploadspec.StoredFileName(name, id))
		got := h.parserForFile(jobpayload.SpeciesSynonymPayload{FilePath: path})
		if got == nil {
			t.Errorf("%s (stored as %s): not recognized", name, filepath.Base(path))
		} else if fmt.Sprintf("%T", got) != fmt.Sprintf("%T", want) {
			t.Errorf("%s: parser %T, want %T", name, got, want)
		}
	}
}

// Tracks are copied into the JBrowse2 data folder under their stored name, so
// files of one name from two bundles must get different names there, and keep
// the .gz ending add_jbrowse_track.sh strips to find the track format.
func TestChildFileName_DistinctAcrossBundles(t *testing.T) {
	a, b := childFileName("bundle-a", "IT23_Ga8.bw"), childFileName("bundle-b", "IT23_Ga8.bw")
	if a == b {
		t.Fatalf("both bundles store %q", a)
	}
	for _, n := range []string{a, b} {
		if !strings.HasPrefix(n, "IT23_Ga8-") || !strings.HasSuffix(n, ".bw.gz") {
			t.Errorf("stored name %q must keep the original stem and end in .bw.gz", n)
		}
	}
	if childFileName("bundle-a", "IT23_Ga8.bw") != a {
		t.Error("a rerun of the bundle job must reproduce the same name")
	}
}

type fakeDeleteFiles struct {
	file    *entity.UploadFile
	deleted bool
}

func (f *fakeDeleteFiles) FindByID(context.Context, string) (*entity.UploadFile, error) {
	return f.file, nil
}

func (f *fakeDeleteFiles) SoftDelete(context.Context, string, string) error {
	f.deleted = true
	return nil
}

type fakeDeleteIndex struct{}

func (fakeDeleteIndex) DeleteByFileID(context.Context, string, string) error { return nil }

// Deleting a synonym or orthology file must remove it from disk. The handlers
// used to join the upload directory onto a path that already includes it, and
// ignored the resulting "not found", so the file stayed on disk forever.
func TestDeleteHandlers_RemoveFileAtStoredPath(t *testing.T) {
	assemblyID := uint64(7)
	type handler interface {
		Handle(context.Context, entity.Job) (json.RawMessage, error)
	}
	cases := map[string]func(files *fakeDeleteFiles) handler{
		"synonym": func(files *fakeDeleteFiles) handler {
			return NewDeleteSynonymHandler(files, fakeBundleVersions{}, fakeDeleteIndex{}, "emobase")
		},
		"orthology": func(files *fakeDeleteFiles) handler {
			return NewDeleteOrthologyTSVHandler(files, fakeBundleVersions{}, fakeDeleteIndex{}, "emobase")
		},
	}
	for name, newHandler := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "public", "uploads", "v1", "file-1a2b3c4d.tsv.gz")
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
			files := &fakeDeleteFiles{file: &entity.UploadFile{ID: "f1", VersionID: 1, AssemblyVersionID: &assemblyID, FilePath: path}}
			raw, _ := json.Marshal(jobpayload.DeleteFilePayload{UploadFileID: "f1", DeletedBy: "admin"})
			p := json.RawMessage(raw)

			if _, err := newHandler(files).Handle(context.Background(), entity.Job{Payload: &p}); err != nil {
				t.Fatal(err)
			}
			if !files.deleted {
				t.Error("upload row was not soft-deleted")
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Errorf("file still on disk at %s (stat err %v)", path, err)
			}
		})
	}
}
