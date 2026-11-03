package assemblyversion

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

// An assembly's name becomes its upload folder, which deleting the assembly
// removes recursively, and which nginx serves under /public/uploads/. So the
// folder must be one plain path segment: no separator, never "." or "..", not
// hidden, and nothing a URL would need to escape.
func TestAssemblyDirName_OneSafeFolder(t *testing.T) {
	for name, want := range map[string]string{
		"Tribolium castaneum / Tcas5.2 (GCF_000002335.3)": "Tribolium castaneum _ Tcas5.2 (GCF_000002335.3)",
		`a/b\c`:      "a_b_c",
		"a//b":       "a_b",
		"../../etc":  "_.._etc",
		".hidden":    "hidden",
		" Tcas 5.2 ": "Tcas 5.2",
		"Tcas #1?":   "Tcas _1_",
		"Café":       "Caf_",
	} {
		got, err := AssemblyDirName(name)
		if err != nil || got != want {
			t.Errorf("AssemblyDirName(%q) = %q, %v; want %q", name, got, err, want)
		}
		if strings.ContainsAny(got, `/\`) || got == "." || got == ".." || strings.HasPrefix(got, ".") {
			t.Errorf("AssemblyDirName(%q) = %q is not one safe folder name", name, got)
		}
	}
	for _, name := range []string{"", "   ", "..", " . . "} {
		if _, err := AssemblyDirName(name); !errors.Is(err, ErrNameNoFolder) {
			t.Errorf("AssemblyDirName(%q): err %v, want ErrNameNoFolder", name, err)
		}
	}
}

// Two assemblies of one version sharing a folder would mix their files, and
// deleting one would remove the other's. A name giving the folder of another
// assembly in the version is refused, ignoring case as a case-insensitive file
// system (and the database's unique key) would. Other versions have their own
// folders, so the same name is fine there.
func TestCreateAssemblyVersion_FolderNameUniqueInVersion(t *testing.T) {
	repo := &memAssemblies{rows: []*entity.AssemblyVersion{{ID: 1, VersionID: 2, Name: "Tcas / 5.2", DirName: "Tcas _ 5.2"}}}
	uc := New(fakeVersions{}, repo, nil, nil, nil, nil, t.TempDir(), "")
	create := func(name string) (*entity.AssemblyVersion, error) {
		return uc.CreateAssemblyVersion(context.Background(), "v1", name, "Tribolium castaneum", "")
	}

	a, err := create("Tcas / 5.2")
	if err != nil {
		t.Fatalf("name used only in another version: refused: %v", err)
	}
	if a.DirName != "Tcas _ 5.2" {
		t.Errorf("folder %q, want %q", a.DirName, "Tcas _ 5.2")
	}
	for _, name := range []string{"Tcas / 5.2", "Tcas _ 5.2", "tcas / 5.2", "Tcas \\ 5.2"} {
		if _, err := create(name); !errors.Is(err, ErrDirNameTaken) {
			t.Errorf("%q: err %v, want ErrDirNameTaken", name, err)
		}
	}
	if _, err := create("Tcas / 5.3"); err != nil {
		t.Errorf("a different folder: refused: %v", err)
	}
}
