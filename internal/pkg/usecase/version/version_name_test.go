package version

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

// createVersions holds the names of existing versions.
type createVersions struct {
	IVersionRepository
	names   []string
	created []string
}

func (r *createVersions) ListNames(context.Context) ([]string, error) { return r.names, nil }

func (r *createVersions) Create(_ context.Context, v *entity.Version) error {
	r.created = append(r.created, v.Name)
	return nil
}

func createVersion(existing []string, name string) (*createVersions, error) {
	repo := &createVersions{names: existing}
	_, err := New(repo, nil, nil, nil, nil, nil, "", "").CreateVersion(context.Background(), name)
	return repo, err
}

// A version name is used raw as the version's upload folder, which
// DeleteVersion removes recursively: "." would make that delete wipe every
// version's uploads, ".." the folder above. Any letter case and inner spaces
// are fine.
func TestCreateVersion_NameMustBeASafeFolderName(t *testing.T) {
	for _, name := range []string{
		"", ".", "..", "../uploads", "a/b", `a\b`, ".hidden", "_v1", "-v1", " v1", "v1 ", "tcas#5.2",
		strings.Repeat("a", 65),
	} {
		repo, err := createVersion(nil, name)
		if !errors.Is(err, ErrInvalidVersionName) || len(repo.created) != 0 {
			t.Errorf("%q: err %v, created %v; want ErrInvalidVersionName and nothing created", name, err, repo.created)
		}
	}
	for _, name := range []string{"Tcas 5.2", "tcas_5.2", "Dmel R6.54", "v1", strings.Repeat("a", 64)} {
		if _, err := createVersion(nil, name); err != nil {
			t.Errorf("%q: rejected: %v", name, err)
		}
	}
}

// Elasticsearch index names lowercase the version name and turn a run of
// spaces into "_". Two versions whose names match that way would share every
// index and alias, so each would search the other's data, and deleting one
// would delete both versions' indexes. Such a name is rejected, naming the
// version it clashes with.
func TestCreateVersion_RejectsNameSharingAnotherVersionsIndexes(t *testing.T) {
	existing := []string{"Tcas 5.2"}

	if _, err := createVersion(existing, "Tcas 5.2"); !errors.Is(err, ErrVersionAlreadyExists) || err.Error() != ErrVersionAlreadyExists.Error() {
		t.Errorf("exact duplicate: err %v, want plain %q", err, ErrVersionAlreadyExists)
	}
	for _, name := range []string{"tcas 5.2", "TCAS 5.2", "tcas_5.2", "Tcas  5.2"} {
		repo, err := createVersion(existing, name)
		if !errors.Is(err, ErrVersionAlreadyExists) || !strings.Contains(err.Error(), `"Tcas 5.2"`) || len(repo.created) != 0 {
			t.Errorf("%q: err %v, created %v; want ErrVersionAlreadyExists naming \"Tcas 5.2\"", name, err, repo.created)
		}
	}
	if _, err := createVersion(existing, "Tcas 5.3"); err != nil {
		t.Errorf("a different version: rejected: %v", err)
	}
}
