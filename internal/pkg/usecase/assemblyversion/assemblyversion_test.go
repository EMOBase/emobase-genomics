package assemblyversion

import (
	"context"
	"errors"
	"testing"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

type fakeVersions struct{}

func (fakeVersions) FindByName(_ context.Context, name string) (*entity.Version, error) {
	return &entity.Version{ID: 1, Name: name}, nil
}

// fakeAssemblies returns every id as an assembly of Database Version 2, so for
// version "v1" each one is from another version.
type fakeAssemblies struct {
	IAssemblyVersionRepository
}

// memAssemblies stores created rows so a test can see what CreateAssemblyVersion
// wrote.
type memAssemblies struct {
	IAssemblyVersionRepository
	rows []*entity.AssemblyVersion
}

func (m *memAssemblies) Create(_ context.Context, a *entity.AssemblyVersion) error {
	a.ID = uint64(len(m.rows) + 1)
	m.rows = append(m.rows, a)
	return nil
}

func (fakeAssemblies) FindByID(_ context.Context, id uint64) (*entity.AssemblyVersion, error) {
	return &entity.AssemblyVersion{ID: id, VersionID: 2, Species: "Tcas"}, nil
}

// An assembly id is only meaningful inside its own Database Version. Using an
// id from another version must read as not found, and must not reach the job,
// upload or ES repositories: they are nil here, so reaching them panics.
func TestDeleteAssemblyVersion_IDFromAnotherVersionIsNotFound(t *testing.T) {
	uc := New(fakeVersions{}, fakeAssemblies{}, nil, nil, nil, t.TempDir())

	err := uc.DeleteAssemblyVersion(context.Background(), "v1", 8)
	if !errors.Is(err, ErrAssemblyVersionNotFound) {
		t.Fatalf("got %v, want ErrAssemblyVersionNotFound", err)
	}
}

// Several assemblies of one species may share a Database Version: a species code
// does not identify an assembly, so creating a second one must not be refused.
func TestCreateAssemblyVersion_AllowsSameSpeciesTwice(t *testing.T) {
	repo := &memAssemblies{}
	uc := New(fakeVersions{}, repo, nil, nil, nil, t.TempDir())

	first, err := uc.CreateAssemblyVersion(context.Background(), "v1", "Tcas Tcas5.2", "Tcas")
	if err != nil {
		t.Fatal(err)
	}
	second, err := uc.CreateAssemblyVersion(context.Background(), "v1", "Tcas Tcas6", "Tcas")
	if err != nil {
		t.Fatalf("second assembly of Tcas refused: %v", err)
	}
	if first.ID == second.ID {
		t.Errorf("both assemblies got id %d", first.ID)
	}
}
