package assemblyversion

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func (m *memAssemblies) FindByID(_ context.Context, id uint64) (*entity.AssemblyVersion, error) {
	if id == 0 || id > uint64(len(m.rows)) {
		return nil, nil
	}
	a := *m.rows[id-1]
	return &a, nil
}

func (m *memAssemblies) Update(_ context.Context, a *entity.AssemblyVersion) error {
	row := m.rows[a.ID-1]
	row.Name, row.InfoURL, row.UpdatedBy = a.Name, a.InfoURL, a.UpdatedBy
	return nil
}

func (m *memAssemblies) UpdateImagePath(_ context.Context, id uint64, imagePath *string, _ string) error {
	m.rows[id-1].ImagePath = imagePath
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

	first, err := uc.CreateAssemblyVersion(context.Background(), "v1", "Tcas Tcas5.2", "Tcas", "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := uc.CreateAssemblyVersion(context.Background(), "v1", "Tcas Tcas6", "Tcas", "")
	if err != nil {
		t.Fatalf("second assembly of Tcas refused: %v", err)
	}
	if first.ID == second.ID {
		t.Errorf("both assemblies got id %d", first.ID)
	}
}

// The info link is rendered as a link on the public site, so anything but an
// absolute http(s) URL is refused: a javascript: or data: URL would run script
// when clicked, and a relative one would point into this site instead.
func TestCreateAssemblyVersion_InfoURLMustBeAbsoluteHTTP(t *testing.T) {
	bad := []string{
		"javascript:alert(1)",
		"data:text/html,<script>alert(1)</script>",
		"ftp://example.org/tcas",
		"/details/tcas",
		"example.org/tcas",
		"https://",
		"https://example.org/" + strings.Repeat("a", maxInfoURLLength),
	}
	for _, raw := range bad {
		repo := &memAssemblies{}
		uc := New(fakeVersions{}, repo, nil, nil, nil, t.TempDir())
		if _, err := uc.CreateAssemblyVersion(context.Background(), "v1", "Tcas6", "Tcas", raw); !errors.Is(err, ErrInvalidInfoURL) {
			t.Errorf("infoUrl %.40q: got %v, want ErrInvalidInfoURL", raw, err)
		}
		if len(repo.rows) != 0 {
			t.Errorf("infoUrl %.40q: assembly was stored anyway", raw)
		}
	}

	repo := &memAssemblies{}
	uc := New(fakeVersions{}, repo, nil, nil, nil, t.TempDir())
	a, err := uc.CreateAssemblyVersion(context.Background(), "v1", "Tcas6", "Tcas", "https://example.org/tcas?x=1")
	if err != nil {
		t.Fatal(err)
	}
	if a.InfoURL == nil || *a.InfoURL != "https://example.org/tcas?x=1" {
		t.Errorf("infoUrl = %v, want the given https URL", a.InfoURL)
	}
}

// PATCH is partial: a field left out keeps its value, and "" is how a client
// removes the optional info link (null cannot be told apart from left out).
func TestUpdateAssemblyVersion_EmptyInfoURLClearsLinkAndKeepsName(t *testing.T) {
	repo := &memAssemblies{}
	uc := New(fakeVersions{}, repo, nil, nil, nil, t.TempDir())
	a, err := uc.CreateAssemblyVersion(context.Background(), "v1", "Tcas6", "Tcas", "https://example.org/tcas")
	if err != nil {
		t.Fatal(err)
	}

	empty := ""
	got, err := uc.UpdateAssemblyVersion(context.Background(), "v1", a.ID, AssemblyVersionUpdate{InfoURL: &empty})
	if err != nil {
		t.Fatal(err)
	}
	if got.InfoURL != nil {
		t.Errorf("infoUrl = %q, want cleared", *got.InfoURL)
	}
	if got.Name != "Tcas6" {
		t.Errorf("name = %q, want it unchanged", got.Name)
	}
}

var pngBytes = append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...)

// The image is served by nginx from the site's own origin, so only raster
// types are stored. SVG can carry scripts, so it is refused, and nothing is
// written to disk.
func TestSetAssemblyVersionImage_RejectsSVG(t *testing.T) {
	repo := &memAssemblies{}
	dir := t.TempDir()
	uc := New(fakeVersions{}, repo, nil, nil, nil, dir)
	a, err := uc.CreateAssemblyVersion(context.Background(), "v1", "Tcas6", "Tcas", "")
	if err != nil {
		t.Fatal(err)
	}

	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	_, err = uc.SetAssemblyVersionImage(context.Background(), "v1", a.ID, bytes.NewReader(svg), int64(len(svg)))
	if !errors.Is(err, ErrUnsupportedImageType) {
		t.Fatalf("got %v, want ErrUnsupportedImageType", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("upload dir not empty after a refused image: %v", entries)
	}
}

// A replaced image must not linger on disk, and every image must live inside
// the assembly's own upload directory: deleting the assembly or its Database
// Version removes that directory, which is the only cleanup images get.
func TestSetAssemblyVersionImage_ReplaceRemovesOldFile(t *testing.T) {
	repo := &memAssemblies{}
	dir := t.TempDir()
	uc := New(fakeVersions{}, repo, nil, nil, nil, dir)
	a, err := uc.CreateAssemblyVersion(context.Background(), "v1", "Tcas6", "Tcas", "")
	if err != nil {
		t.Fatal(err)
	}

	first, err := uc.SetAssemblyVersionImage(context.Background(), "v1", a.ID, bytes.NewReader(pngBytes), int64(len(pngBytes)))
	if err != nil {
		t.Fatal(err)
	}
	second, err := uc.SetAssemblyVersionImage(context.Background(), "v1", a.ID, bytes.NewReader(pngBytes), int64(len(pngBytes)))
	if err != nil {
		t.Fatal(err)
	}

	if *first.ImagePath == *second.ImagePath {
		t.Fatalf("replacement reused path %s, so a cached copy of the old image would be shown", *second.ImagePath)
	}
	if _, err := os.Stat(*first.ImagePath); !os.IsNotExist(err) {
		t.Errorf("old image %s still on disk", *first.ImagePath)
	}
	got, err := os.ReadFile(*second.ImagePath)
	if err != nil || !bytes.Equal(got, pngBytes) {
		t.Errorf("new image not stored intact: %v", err)
	}
	assemblyDir := filepath.Join(dir, "v1", a.AssemblyID())
	if filepath.Dir(*second.ImagePath) != assemblyDir || filepath.Ext(*second.ImagePath) != ".png" {
		t.Errorf("image stored at %s, want a .png inside %s", *second.ImagePath, assemblyDir)
	}
}

// The size limit is checked before anything is read or written.
func TestSetAssemblyVersionImage_TooLarge(t *testing.T) {
	repo := &memAssemblies{}
	uc := New(fakeVersions{}, repo, nil, nil, nil, t.TempDir())
	_, err := uc.SetAssemblyVersionImage(context.Background(), "v1", 1, bytes.NewReader(pngBytes), MaxImageSize+1)
	if !errors.Is(err, ErrImageTooLarge) {
		t.Fatalf("got %v, want ErrImageTooLarge", err)
	}
}
