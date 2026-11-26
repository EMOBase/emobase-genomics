package upload

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	tusd "github.com/tus/tusd/v2/pkg/handler"
)

// memUploads keeps upload_files rows in memory, with the same notion of an
// assembly's "latest completed file" as the MySQL repository.
type memUploads struct {
	IUploadFileRepository
	rows map[string]*entity.UploadFile
}

func (m *memUploads) UpdateStatus(_ context.Context, id string, status entity.UploadStatus) error {
	m.rows[id].UploadStatus = status
	return nil
}

func (m *memUploads) FindLatestCompletedByAssemblyVersionAndType(_ context.Context, asm uint64, fileType string) (*entity.UploadFile, error) {
	var latest *entity.UploadFile
	for _, f := range m.rows {
		if *f.AssemblyVersionID == asm && f.FileType == fileType && f.UploadStatus == entity.UploadStatusCompleted &&
			f.DeletedAt == nil && (latest == nil || f.CreatedAt.After(latest.CreatedAt)) {
			latest = f
		}
	}
	if latest == nil {
		return nil, nil
	}
	c := *latest
	return &c, nil
}

func (m *memUploads) ListByAssemblyVersionID(_ context.Context, asm uint64) ([]entity.UploadFile, error) {
	var out []entity.UploadFile
	for _, f := range m.rows {
		if *f.AssemblyVersionID == asm && f.DeletedAt == nil {
			out = append(out, *f)
		}
	}
	return out, nil
}

func (m *memUploads) SoftDelete(_ context.Context, id, deletedBy string) error {
	now := time.Now()
	m.rows[id].DeletedAt, m.rows[id].DeletedBy = &now, &deletedBy
	return nil
}

// jobsOfFiles also answers FindByFileIDs with jobs that already exist.
type jobsOfFiles struct {
	*fakeUploadJobs
	existing []entity.Job
}

func (j jobsOfFiles) FindByFileIDs(_ context.Context, ids []string) ([]entity.Job, error) {
	var out []entity.Job
	for _, job := range j.existing {
		if slices.Contains(ids, *job.FileID) {
			out = append(out, job)
		}
	}
	return out, nil
}

type replaceFixture struct {
	t       *testing.T
	uc      *UseCase
	jobs    *fakeUploadJobs
	uploads *memUploads
}

func newReplaceFixture(t *testing.T, existing []entity.Job) *replaceFixture {
	uc, jobs := newFinishTestUseCase(t)
	uploads := &memUploads{rows: map[string]*entity.UploadFile{}}
	uc.uploadRepo = uploads
	uc.jobRepo = jobsOfFiles{fakeUploadJobs: jobs, existing: existing}
	return &replaceFixture{t: t, uc: uc, jobs: jobs, uploads: uploads}
}

// stored adds a completed upload whose file is on disk, as an earlier upload
// would have left it.
func (f *replaceFixture) stored(id string, asm uint64, fileType, fileName string, created time.Time) string {
	f.t.Helper()
	meta := assemblyMeta(fileType, fileName, nil)
	if asm != 7 { // assemblyMeta describes assembly 7
		meta["_assemblyDir"] = fmt.Sprintf("Other %d", asm)
	}
	path := f.uc.storedPath(meta)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("\x1f\x8bold"), 0o644); err != nil {
		f.t.Fatal(err)
	}
	f.uploads.rows[id] = &entity.UploadFile{ID: id, AssemblyVersionID: &asm, FileType: fileType, FilePath: path,
		UploadStatus: entity.UploadStatusCompleted, CreatedAt: created}
	return path
}

// upload runs a new upload of assembly 7 through the finish hook, as tusd does
// once the file is fully received.
func (f *replaceFixture) upload(id, fileType, fileName string, created time.Time) string {
	f.t.Helper()
	asm := uint64(7)
	meta := assemblyMeta(fileType, fileName, nil)
	f.uploads.rows[id] = &entity.UploadFile{ID: id, AssemblyVersionID: &asm, FileType: fileType,
		FilePath: f.uc.storedPath(meta), UploadStatus: entity.UploadStatusUploading, CreatedAt: created}
	return finishUpload(f.t, f.uc, f.jobs, id, meta, "\x1f\x8bnew")
}

func (f *replaceFixture) assertKept(path, id string) {
	f.t.Helper()
	if _, err := os.Stat(path); err != nil {
		f.t.Errorf("%s: file should be kept: %v", id, err)
	}
	if f.uploads.rows[id].DeletedAt != nil {
		f.t.Errorf("%s: record should not be soft-deleted", id)
	}
}

// An assembly keeps one file of each single-file type. A re-upload under
// another name doesn't overwrite the previous file, so finishing it must remove
// the previous file and soft-delete its record, or it stays in the public folder
// and still counts in listings and the version size. Untouched: other types,
// other assemblies, and a replaced file that a pending or running job still
// reads, which stays until the next upload of its type (a failed job does not
// keep a file).
func TestUploadFinish_RemovesReplacedSingleFile(t *testing.T) {
	t0 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	running := "busy0000000000000000000000000002"
	f := newReplaceFixture(t, []entity.Job{
		{FileID: strPtr("old00000000000000000000000000001"), Type: entity.JobTypeRNAFNA, Status: entity.JobStatusFailed},
		{FileID: &running, Type: entity.JobTypeRNAFNASetupBlast, Status: entity.JobStatusRunning},
	})
	old := f.stored("old00000000000000000000000000001", 7, entity.FileTypeRNAFNA, "rna_v1.fna.gz", t0)
	busy := f.stored(running, 7, entity.FileTypeRNAFNA, "rna_v2.fna.gz", t0.Add(time.Hour))
	protein := f.stored("prot0000000000000000000000000003", 7, entity.FileTypeProteinFAA, "protein.faa.gz", t0)
	otherAssembly := f.stored("asm80000000000000000000000000004", 8, entity.FileTypeRNAFNA, "rna.fna.gz", t0)

	newPath := f.upload("new00000000000000000000000000005", entity.FileTypeRNAFNA, "rna.fna.gz", t0.Add(2*time.Hour))

	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("replaced file should be removed from disk: %v", err)
	}
	if f.uploads.rows["old00000000000000000000000000001"].DeletedAt == nil {
		t.Error("replaced file's record should be soft-deleted")
	}
	f.assertKept(busy, running)
	f.assertKept(protein, "prot0000000000000000000000000003")
	f.assertKept(otherAssembly, "asm80000000000000000000000000004")
	f.assertKept(newPath, "new00000000000000000000000000005")
}

// A re-upload under the same name replaces the previous file in place, so the
// latest file keeps the name people look for. The move overwrites the old file,
// and the cleanup must then only soft-delete the old record: removing the old
// record's path would delete the new file.
func TestUploadFinish_SameNameReplacesSingleFile(t *testing.T) {
	t0 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	f := newReplaceFixture(t, nil)
	old := f.stored("old00000000000000000000000000001", 7, entity.FileTypeRNAFNA, "rna.fna.gz", t0)

	newPath := f.upload("new00000000000000000000000000002", entity.FileTypeRNAFNA, "rna.fna.gz", t0.Add(time.Hour))

	if newPath != old || filepath.Base(newPath) != "rna.fna.gz" {
		t.Errorf("stored at %s, want the uploaded name in place of the old file, %s", newPath, old)
	}
	if got, err := os.ReadFile(newPath); err != nil || string(got) != "\x1f\x8bnew" {
		t.Errorf("%s: got %q, %v; want the new upload's content", newPath, got, err)
	}
	if f.uploads.rows["old00000000000000000000000000001"].DeletedAt == nil {
		t.Error("replaced file's record should be soft-deleted")
	}
	f.assertKept(newPath, "new00000000000000000000000000002")
}

// A file of a single-file type replaces only its own type's file: if the name
// belongs to another type's file (a GFF stored as a JBrowse2 track), uploading
// must not overwrite it.
func TestPreUpload_SingleFileDoesNotReplaceAnotherTypesFile(t *testing.T) {
	f := newReplaceFixture(t, nil)
	f.uc.versionRepo, f.uc.assemblyVersionRepo = fakeUploadVersions{}, fakeUploadAssemblies{}
	f.uc.jobRepo = busyFileTypes{fakeUploadJobs: f.jobs} // no job is running
	meta := tusd.MetaData{"fileType": entity.FileTypeGenomicGFF, "fileName": "genomic.gff.gz", "version": "v1", "assembly": "7",
		"geneIDKey": "ID", "trimPrefixChars": "0", "trimSuffixChars": "0"}
	// The fake assembly 7 has no folder name, so its files sit in the version folder.
	path := filepath.Join(f.uc.uploadDir, "v1", "genomic.gff.gz")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("\x1f\x8bold"), 0o644); err != nil {
		t.Fatal(err)
	}
	storedAs := func(fileType string) {
		asm := uint64(7)
		f.uploads.rows = map[string]*entity.UploadFile{"old00000000000000000000000000001": {ID: "old00000000000000000000000000001",
			AssemblyVersionID: &asm, FileType: fileType, FilePath: path, UploadStatus: entity.UploadStatusCompleted}}
	}

	storedAs(entity.FileTypeJBrowseTrack)
	_, err := preUpload(f.uc, meta)
	requireStatus(t, err, http.StatusConflict, `already has a jbrowse.track file named "genomic.gff.gz"`)

	storedAs(entity.FileTypeGenomicGFF)
	if _, err := preUpload(f.uc, meta); err != nil {
		t.Errorf("re-upload of the assembly's own genomic.gff refused: %v", err)
	}
}

// Uploads can finish out of order. If a newer upload of the type has already
// completed, the one finishing now is already replaced: it must not remove
// anything, above all not the newer file, which is the one in use.
func TestUploadFinish_OlderUploadFinishingLateRemovesNothing(t *testing.T) {
	t0 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	f := newReplaceFixture(t, nil)
	older := f.stored("old00000000000000000000000000001", 7, entity.FileTypeGenomicGFF, "genomic_v1.gff.gz", t0)
	newer := f.stored("newr0000000000000000000000000003", 7, entity.FileTypeGenomicGFF, "genomic_v3.gff.gz", t0.Add(2*time.Hour))

	// The late upload, created between the two, has just been marked completed.
	// Its cleanup is called directly: a GFF finish also enqueues synonym and
	// JBrowse2 jobs, which these fakes do not cover.
	asm, late := uint64(7), "late0000000000000000000000000002"
	f.uploads.rows[late] = &entity.UploadFile{ID: late, AssemblyVersionID: &asm, FileType: entity.FileTypeGenomicGFF,
		UploadStatus: entity.UploadStatusCompleted, CreatedAt: t0.Add(time.Hour)}
	lateMeta := assemblyMeta(entity.FileTypeGenomicGFF, "genomic_v2.gff.gz", nil)
	f.uc.removeReplacedFiles(context.Background(), late, lateMeta, f.uc.storedPath(lateMeta))

	f.assertKept(newer, "newr0000000000000000000000000003")
	f.assertKept(older, "old00000000000000000000000000001")
}

func strPtr(s string) *string { return &s }
