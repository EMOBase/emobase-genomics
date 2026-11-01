package upload

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	tusd "github.com/tus/tusd/v2/pkg/handler"
)

type storedVersion struct{}

// FindByName ignores case, as MySQL's collation does, and returns the stored name.
func (storedVersion) FindByName(context.Context, string) (*entity.Version, error) {
	return &entity.Version{ID: 1, Name: "MultiSpecies"}, nil
}

type namedAssembly struct{}

func (namedAssembly) FindByID(context.Context, uint64) (*entity.AssemblyVersion, error) {
	return &entity.AssemblyVersion{ID: 7, VersionID: 1, SpeciesCode: "Tcas",
		Name: "Tribolium castaneum / Tcas5.2", DirName: "Tribolium castaneum _ Tcas5.2"}, nil
}

// An upload is filed under its assembly's folder, named after the assembly
// (its DirName), and its JBrowse2 label reads "{version} — {assembly}" from
// the stored names, whatever case the client typed the version in.
func TestPreUpload_AssemblyFolderAndLabelFromStoredNames(t *testing.T) {
	uc, _ := newTestUseCase()
	uc.versionRepo, uc.assemblyVersionRepo = storedVersion{}, namedAssembly{}

	changes, err := preUpload(uc, tusd.MetaData{"fileType": entity.FileTypeJBrowseTrack, "fileName": "cov.bw.gz",
		"version": "multispecies", "assembly": "7", "trackName": "Coverage"})
	if err != nil {
		t.Fatal(err)
	}
	if got := changes.MetaData["_assemblyDir"]; got != "Tribolium castaneum _ Tcas5.2" {
		t.Errorf("_assemblyDir %q, want the assembly's folder name", got)
	}
	if got := changes.MetaData["_displayLabel"]; got != "MultiSpecies — Tribolium castaneum / Tcas5.2" {
		t.Errorf("_displayLabel %q, want the stored version and assembly names", got)
	}
}

// An upload created before assemblies had named folders carries only the old
// "v{version}a{id}" key, the folder its assembly's files already live in.
func TestStoredPath_OlderUploadKeepsAssemblyIDFolder(t *testing.T) {
	uc, _ := newTestUseCase()
	uc.uploadDir = "/up"
	meta := tusd.MetaData{"version": "v1", "fileName": "rna.fna.gz", "_assemblyKey": "v1a7"}
	if dir := filepath.Dir(uc.storedPath("aaaaaaaa", meta)); dir != "/up/v1/v1a7" {
		t.Errorf("stored in %s, want /up/v1/v1a7", dir)
	}
	meta["_assemblyDir"] = "Tribolium"
	if dir := filepath.Dir(uc.storedPath("aaaaaaaa", meta)); dir != "/up/v1/Tribolium" {
		t.Errorf("stored in %s, want the assembly's folder /up/v1/Tribolium", dir)
	}
}
