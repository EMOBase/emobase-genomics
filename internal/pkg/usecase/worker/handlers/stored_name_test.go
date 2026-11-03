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

// A bundle is extracted into a folder named after its archive, so people can
// find it, plus a tag from the bundle's ID: a later bundle of the same name
// gets its own folder instead of writing over the earlier bundle's files, and a
// rerun of the job finds the folder it already wrote.
func TestBundleDirName(t *testing.T) {
	const id = "1a2b3c4d5e6f708192a3b4c5d6e7f809" // tusd ID: 32 hex characters
	for archive, want := range map[string]string{
		"tracks.tar.gz":      "tracks-1a2b3c4d",
		"Tracks.TAR.GZ":      "Tracks-1a2b3c4d",
		"orthology.v2.zip":   "orthology.v2-1a2b3c4d",
		"RNA-seq tracks.zip": "RNA-seq tracks-1a2b3c4d",
	} {
		if got := bundleDirName(archive, id); got != want {
			t.Errorf("bundleDirName(%q) = %q, want %q", archive, got, want)
		}
	}
	if bundleDirName("tracks.tar.gz", "aaaaaaaa11111111") == bundleDirName("tracks.tar.gz", "bbbbbbbb22222222") {
		t.Error("two bundles of one name must get different folders")
	}
	if bundleDirName("tracks.tar.gz", id) != bundleDirName("tracks.tar.gz", id) {
		t.Error("a rerun of the bundle job must find the same folder")
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
