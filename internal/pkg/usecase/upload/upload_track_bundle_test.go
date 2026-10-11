package upload

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	"github.com/EMOBase/emobase-genomics/internal/pkg/jobpayload"
	tusd "github.com/tus/tusd/v2/pkg/handler"
)

type fakeUploadVersions struct{}

func (fakeUploadVersions) FindByName(_ context.Context, name string) (*entity.Version, error) {
	return &entity.Version{ID: 1, Name: name}, nil
}

type fakeUploadAssemblies struct{}

func (fakeUploadAssemblies) FindByID(_ context.Context, id uint64) (*entity.AssemblyVersion, error) {
	switch id {
	case 7:
		return &entity.AssemblyVersion{ID: 7, VersionID: 1, Species: "Tcas", Name: "Tribolium"}, nil
	case 8:
		// Belongs to another Database Version, so it must not resolve for version 1.
		return &entity.AssemblyVersion{ID: 8, VersionID: 2, Species: "Tcas", Name: "Other"}, nil
	}
	return nil, nil
}

// fakeUploadJobs records created jobs. Every other repository method is unused on
// the paths these tests take, so the embedded nil interface is never called.
type fakeUploadJobs struct {
	IJobRepository
	created []*entity.Job
}

func (f *fakeUploadJobs) Create(_ context.Context, j *entity.Job) error {
	f.created = append(f.created, j)
	return nil
}

func newTestUseCase() (*UseCase, *fakeUploadJobs) {
	jobs := &fakeUploadJobs{}
	return &UseCase{
		versionRepo:         fakeUploadVersions{},
		assemblyVersionRepo: fakeUploadAssemblies{},
		jobRepo:             jobs,
	}, jobs
}

func trackBundleMeta(extra map[string]string) tusd.MetaData {
	meta := tusd.MetaData{
		"fileType": entity.FileTypeJBrowseTrackBundle,
		"fileName": "bundle.tar.gz",
		"version":  "v1",
	}
	for k, v := range extra {
		meta[k] = v
	}
	return meta
}

func preUpload(uc *UseCase, meta tusd.MetaData) (tusd.FileInfoChanges, error) {
	_, changes, err := uc.handlePreUploadCreate(tusd.HookEvent{
		Context: context.Background(),
		Upload:  tusd.FileInfo{MetaData: meta},
	})
	return changes, err
}

func requireStatus(t *testing.T, err error, status int, wantMsg string) {
	t.Helper()
	var tusErr tusd.Error
	if !errors.As(err, &tusErr) {
		t.Fatalf("got %v, want a tus error with status %d", err, status)
	}
	if tusErr.HTTPResponse.StatusCode != status {
		t.Errorf("status %d, want %d", tusErr.HTTPResponse.StatusCode, status)
	}
	if !strings.Contains(tusErr.Message, wantMsg) {
		t.Errorf("message %q, want it to mention %q", tusErr.Message, wantMsg)
	}
}

// A track bundle covers one assembly, so the upload must name it. The manifest
// cannot carry it: the manifest is inside the archive, and only a tus field can
// be checked before anything is stored.
func TestTrackBundleUpload_RequiresAssembly(t *testing.T) {
	uc, _ := newTestUseCase()
	_, err := preUpload(uc, trackBundleMeta(nil))
	requireStatus(t, err, http.StatusBadRequest, `"assembly"`)
}

// A species code does not pick an assembly: one Database Version can hold several
// assemblies of the same species. The metadata must be the assembly id.
func TestTrackBundleUpload_RejectsSpeciesCodeAsAssembly(t *testing.T) {
	uc, _ := newTestUseCase()
	_, err := preUpload(uc, trackBundleMeta(map[string]string{"assembly": "Tcas"}))
	requireStatus(t, err, http.StatusBadRequest, "assembly id")
}

func TestTrackBundleUpload_RejectsUnknownAssembly(t *testing.T) {
	uc, _ := newTestUseCase()
	_, err := preUpload(uc, trackBundleMeta(map[string]string{"assembly": "999"}))
	requireStatus(t, err, http.StatusBadRequest, "not found")
}

// An assembly id from another Database Version must not resolve, or an upload
// could file its data under an assembly it does not belong to.
func TestTrackBundleUpload_RejectsAssemblyFromAnotherVersion(t *testing.T) {
	uc, _ := newTestUseCase()
	_, err := preUpload(uc, trackBundleMeta(map[string]string{"assembly": "8"}))
	requireStatus(t, err, http.StatusBadRequest, "not found")
}

func TestTrackBundleUpload_ResolvesAssemblyForArchive(t *testing.T) {
	uc, _ := newTestUseCase()
	changes, err := preUpload(uc, trackBundleMeta(map[string]string{"assembly": "7"}))
	if err != nil {
		t.Fatal(err)
	}
	// The archive is stored under this assembly's folder, and the bundle row and
	// job carry its ID, so the children can inherit it.
	if got := changes.MetaData["_assemblyVersionID"]; got != "7" {
		t.Errorf("_assemblyVersionID = %q, want 7", got)
	}
	if got := changes.MetaData["_assemblySpecies"]; got != "Tcas" {
		t.Errorf("_assemblySpecies = %q, want Tcas", got)
	}
}

// A single track upload must still stamp its assembly on the job. The bundle
// path shares the job builder, so this guards against either path dropping it.
func TestSingleTrackUpload_JobCarriesAssembly(t *testing.T) {
	uc, jobs := newTestUseCase()
	meta := tusd.MetaData{
		"fileType":           entity.FileTypeJBrowseTrack,
		"fileName":           "t.gff.gz",
		"version":            "v1",
		"trackName":          "Pre-blastoderm",
		"_versionID":         "1",
		"_assemblyVersionID": "7",
		"_assemblySpecies":   "Tcas",
	}
	if _, err := uc.enqueueProcessJob(context.Background(), "upload-1", meta, "/tmp/t.gff.gz"); err != nil {
		t.Fatal(err)
	}
	if len(jobs.created) != 1 {
		t.Fatalf("created %d jobs, want 1", len(jobs.created))
	}
	j := jobs.created[0]
	if j.AssemblyVersionID == nil || *j.AssemblyVersionID != 7 {
		t.Fatalf("job assembly %v, want 7", j.AssemblyVersionID)
	}
	var p jobpayload.JBrowseTrackPayload
	if err := json.Unmarshal(*j.Payload, &p); err != nil {
		t.Fatal(err)
	}
	if p.AssemblyVersionID != 7 || p.VersionID != 1 {
		t.Errorf("payload ids = (%d, %d), want (1, 7)", p.VersionID, p.AssemblyVersionID)
	}
}

// Orthology bundles have no assembly, and reach the same job builder as the
// per-assembly file types. Building their job must not dereference a nil
// assembly id.
func TestOrthologyBundleUpload_EnqueuesWithoutAssembly(t *testing.T) {
	uc, jobs := newTestUseCase()
	meta := tusd.MetaData{
		"fileType":   entity.FileTypeOrthologyBundle,
		"fileName":   "bundle.zip",
		"version":    "v1",
		"_versionID": "1",
	}
	if _, err := uc.enqueueProcessJob(context.Background(), "upload-2", meta, "/tmp/bundle.zip"); err != nil {
		t.Fatal(err)
	}
	if len(jobs.created) != 1 {
		t.Fatalf("created %d jobs, want 1", len(jobs.created))
	}
	if jobs.created[0].AssemblyVersionID != nil {
		t.Errorf("orthology bundle job has assembly %d, want none", *jobs.created[0].AssemblyVersionID)
	}
}
