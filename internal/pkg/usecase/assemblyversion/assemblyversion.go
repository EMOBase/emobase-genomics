package assemblyversion

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/EMOBase/emobase-genomics/internal/pkg/auth"
	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	ucversion "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/version"
	"github.com/rs/zerolog/log"
)

// deleteJBrowseVersionScript removes one assembly's JBrowse2 assembly,
// tracks, text-search entries, and default-session views, keyed by its
// opaque AssemblyID — despite the "version" in its name (shared with
// usecase/version, which loops it once per assembly for a whole-Database-
// Version delete), it always operates on exactly one assembly identifier.
const deleteJBrowseVersionScript = "/app/scripts/delete_jbrowse_version.sh"

var (
	ErrVersionNotFound              = ucversion.ErrVersionNotFound
	ErrAssemblyVersionNotFound      = errors.New("assembly version not found")
	ErrAssemblyVersionHasActiveJobs = errors.New("assembly version has active jobs")
	ErrEmptyName                    = errors.New("name must not be empty")
	ErrInvalidInfoURL               = fmt.Errorf("infoUrl must be an absolute http or https URL of at most %d characters", maxInfoURLLength)
	ErrImageTooLarge                = fmt.Errorf("image must be at most %d MiB", MaxImageSize>>20)
	ErrUnsupportedImageType         = errors.New("image must be a PNG, JPEG or WebP file")
)

// maxInfoURLLength matches the info_url column.
const maxInfoURLLength = 2048

// MaxImageSize caps an uploaded assembly image.
const MaxImageSize = 5 << 20

// imageExtensions maps each accepted image type, sniffed from the file's own
// bytes, to the extension it is stored with. SVG is left out on purpose: it
// can carry scripts, and nginx serves the image from the site's own origin.
var imageExtensions = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/webp": ".webp",
}

// validateInfoURL accepts only absolute http(s) URLs. The link is shown on the
// public site, where a javascript: or data: URL would run script when clicked.
func validateInfoURL(raw string) error {
	if len(raw) > maxInfoURLLength {
		return ErrInvalidInfoURL
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ErrInvalidInfoURL
	}
	return nil
}

// AssemblyVersionItem is the list-view representation of an Assembly Version.
type AssemblyVersionItem struct {
	entity.AssemblyVersion
	Status string `json:"status"`
}

type UseCase struct {
	versionRepo         IVersionRepository
	assemblyVersionRepo IAssemblyVersionRepository
	jobRepo             IJobRepository
	uploadFileRepo      IUploadFileRepository
	esRepo              IESRepository
	uploadDir           string
}

func New(versionRepo IVersionRepository, assemblyVersionRepo IAssemblyVersionRepository, jobRepo IJobRepository, uploadFileRepo IUploadFileRepository, esRepo IESRepository, uploadDir string) *UseCase {
	return &UseCase{
		versionRepo:         versionRepo,
		assemblyVersionRepo: assemblyVersionRepo,
		jobRepo:             jobRepo,
		uploadFileRepo:      uploadFileRepo,
		esRepo:              esRepo,
		uploadDir:           uploadDir,
	}
}

// CreateAssemblyVersion creates an assembly for a species within a Database Version.
// Every Assembly Version is created identically — there is no auto-assigned
// "primary"/"default" one (§2 of the design doc).
// infoURL is optional; "" leaves it unset.
func (uc *UseCase) CreateAssemblyVersion(ctx context.Context, versionName, name, species, infoURL string) (*entity.AssemblyVersion, error) {
	var info *string
	if infoURL != "" {
		if err := validateInfoURL(infoURL); err != nil {
			return nil, err
		}
		info = &infoURL
	}

	v, err := uc.versionRepo.FindByName(ctx, versionName)
	if err != nil {
		return nil, err
	}
	if v == nil {
		return nil, ErrVersionNotFound
	}

	a := &entity.AssemblyVersion{
		VersionID: v.ID,
		Name:      name,
		Species:   species,
		InfoURL:   info,
		CreatedBy: auth.UsernameFromContext(ctx),
		UpdatedBy: auth.UsernameFromContext(ctx),
	}

	if err := uc.assemblyVersionRepo.Create(ctx, a); err != nil {
		return nil, err
	}

	return a, nil
}

// ListAssemblyVersions lists every species within a Database Version, along
// with each one's own status.
func (uc *UseCase) ListAssemblyVersions(ctx context.Context, versionName string) ([]AssemblyVersionItem, error) {
	v, err := uc.versionRepo.FindByName(ctx, versionName)
	if err != nil {
		return nil, err
	}
	if v == nil {
		return nil, ErrVersionNotFound
	}

	assemblies, err := uc.assemblyVersionRepo.ListByVersionID(ctx, v.ID)
	if err != nil {
		return nil, err
	}

	items := make([]AssemblyVersionItem, len(assemblies))
	for i, asm := range assemblies {
		counts, err := uc.jobRepo.StatusCountsByAssemblyVersionID(ctx, asm.ID)
		if err != nil {
			return nil, err
		}
		completedFiles, err := uc.uploadFileRepo.FindLatestCompletedPerTypeByAssemblyVersionID(ctx, asm.ID)
		if err != nil {
			return nil, err
		}
		hasFNA := false
		for _, f := range completedFiles {
			if f.FileType == entity.FileTypeGenomicFNA {
				hasFNA = true
				break
			}
		}
		items[i] = AssemblyVersionItem{
			AssemblyVersion: asm,
			Status:          ucversion.ComputeVersionStatus(counts, hasFNA),
		}
	}

	return items, nil
}

// GetAssemblyVersionDetail returns one Assembly Version's detail: its own
// status plus its per-file-type file/job buckets — mirroring
// usecase/version.GetVersionDetail one level down, and reusing the exact same
// builder so both stay consistent.
func (uc *UseCase) GetAssemblyVersionDetail(ctx context.Context, versionName string, assemblyVersionID uint64) (*ucversion.AssemblyVersionDetail, error) {
	_, asm, err := uc.findAssemblyVersion(ctx, versionName, assemblyVersionID)
	if err != nil {
		return nil, err
	}

	return ucversion.BuildAssemblyVersionDetail(ctx, uc.jobRepo, uc.uploadFileRepo, *asm)
}

// findAssemblyVersion loads a Database Version and one of its assemblies. An
// id from another Database Version is not found here, so the URL cannot reach
// across versions.
func (uc *UseCase) findAssemblyVersion(ctx context.Context, versionName string, assemblyVersionID uint64) (*entity.Version, *entity.AssemblyVersion, error) {
	v, err := uc.versionRepo.FindByName(ctx, versionName)
	if err != nil {
		return nil, nil, err
	}
	if v == nil {
		return nil, nil, ErrVersionNotFound
	}

	asm, err := uc.assemblyVersionRepo.FindByID(ctx, assemblyVersionID)
	if err != nil {
		return nil, nil, err
	}
	if asm == nil || asm.VersionID != v.ID {
		return nil, nil, ErrAssemblyVersionNotFound
	}
	return v, asm, nil
}

// AssemblyVersionUpdate holds the fields PATCH may change; a nil field is left
// as is. species is not here: it is copied into every indexed document at
// upload time, so changing it would leave those documents under the old one.
type AssemblyVersionUpdate struct {
	Name *string
	// InfoURL set to "" clears the link.
	InfoURL *string
}

// UpdateAssemblyVersion applies a partial update to one assembly.
func (uc *UseCase) UpdateAssemblyVersion(ctx context.Context, versionName string, assemblyVersionID uint64, upd AssemblyVersionUpdate) (*entity.AssemblyVersion, error) {
	if upd.Name != nil && strings.TrimSpace(*upd.Name) == "" {
		return nil, ErrEmptyName
	}
	if upd.InfoURL != nil && *upd.InfoURL != "" {
		if err := validateInfoURL(*upd.InfoURL); err != nil {
			return nil, err
		}
	}

	_, asm, err := uc.findAssemblyVersion(ctx, versionName, assemblyVersionID)
	if err != nil {
		return nil, err
	}

	if upd.Name != nil {
		asm.Name = *upd.Name
	}
	if upd.InfoURL != nil {
		asm.InfoURL = upd.InfoURL
		if *upd.InfoURL == "" {
			asm.InfoURL = nil
		}
	}
	asm.UpdatedBy = auth.UsernameFromContext(ctx)

	if err := uc.assemblyVersionRepo.Update(ctx, asm); err != nil {
		return nil, err
	}
	return uc.assemblyVersionRepo.FindByID(ctx, asm.ID)
}

// SetAssemblyVersionImage stores an uploaded image for one assembly, replacing
// any previous one. The file goes into the assembly's own upload directory, so
// deleting the assembly or its Database Version removes it too.
func (uc *UseCase) SetAssemblyVersionImage(ctx context.Context, versionName string, assemblyVersionID uint64, image io.ReadSeeker, size int64) (*entity.AssemblyVersion, error) {
	if size > MaxImageSize {
		return nil, ErrImageTooLarge
	}

	v, asm, err := uc.findAssemblyVersion(ctx, versionName, assemblyVersionID)
	if err != nil {
		return nil, err
	}

	head := make([]byte, 512)
	n, err := io.ReadFull(image, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, err
	}
	ext, ok := imageExtensions[http.DetectContentType(head[:n])]
	if !ok {
		return nil, ErrUnsupportedImageType
	}
	if _, err := image.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	dir := filepath.Join(uc.uploadDir, v.Name, asm.AssemblyID())
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	// A new name per upload, so a replaced image never reuses a cached URL.
	path := filepath.Join(dir, fmt.Sprintf("image-%d%s", time.Now().UnixNano(), ext))
	if err := writeFile(path, image); err != nil {
		return nil, err
	}

	if err := uc.assemblyVersionRepo.UpdateImagePath(ctx, asm.ID, &path, auth.UsernameFromContext(ctx)); err != nil {
		_ = os.Remove(path)
		return nil, err
	}

	if asm.ImagePath != nil {
		if err := os.Remove(*asm.ImagePath); err != nil && !os.IsNotExist(err) {
			log.Ctx(ctx).Warn().Err(err).Str("path", *asm.ImagePath).Msg("failed to remove replaced assembly image")
		}
	}

	return uc.assemblyVersionRepo.FindByID(ctx, asm.ID)
}

// DeleteAssemblyVersionImage removes one assembly's image. Deleting when there
// is no image succeeds, so the call is safe to repeat.
func (uc *UseCase) DeleteAssemblyVersionImage(ctx context.Context, versionName string, assemblyVersionID uint64) error {
	_, asm, err := uc.findAssemblyVersion(ctx, versionName, assemblyVersionID)
	if err != nil {
		return err
	}
	if asm.ImagePath == nil {
		return nil
	}

	if err := uc.assemblyVersionRepo.UpdateImagePath(ctx, asm.ID, nil, auth.UsernameFromContext(ctx)); err != nil {
		return err
	}
	if err := os.Remove(*asm.ImagePath); err != nil && !os.IsNotExist(err) {
		log.Ctx(ctx).Warn().Err(err).Str("path", *asm.ImagePath).Msg("failed to remove assembly image")
	}
	return nil
}

func writeFile(path string, r io.Reader) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}

// DeleteAssemblyVersion deletes one assembly from a Database Version: its
// jobs, upload file rows and on-disk files, ES indexes, its JBrowse2
// assembly/tracks, and the assembly_versions row itself. There is no
// orthology-related guard — deleting an assembly never touches the Database
// Version's shared orthology.tsv data, since orthology belongs to no single
// assembly (§2 of the design doc).
func (uc *UseCase) DeleteAssemblyVersion(ctx context.Context, versionName string, assemblyVersionID uint64) error {
	v, asm, err := uc.findAssemblyVersion(ctx, versionName, assemblyVersionID)
	if err != nil {
		return err
	}

	hasActive, err := uc.jobRepo.HasActiveJobsByAssemblyVersionID(ctx, asm.ID)
	if err != nil {
		return err
	}
	if hasActive {
		return ErrAssemblyVersionHasActiveJobs
	}

	// Collect file paths before deleting records so we can remove them from disk.
	files, err := uc.uploadFileRepo.ListByAssemblyVersionID(ctx, asm.ID)
	if err != nil {
		return err
	}

	if err := uc.jobRepo.DeleteByAssemblyVersionID(ctx, asm.ID); err != nil {
		return err
	}

	if err := uc.uploadFileRepo.HardDeleteByAssemblyVersionID(ctx, asm.ID); err != nil {
		return err
	}

	if err := uc.assemblyVersionRepo.Delete(ctx, asm.ID); err != nil {
		return err
	}

	if err := uc.esRepo.DeleteIndexesByAssemblyVersion(ctx, v.Name, asm.AssemblyID()); err != nil {
		log.Ctx(ctx).Warn().Err(err).Str("version", v.Name).Str("assemblyID", asm.AssemblyID()).Msg("failed to delete ES indexes during assembly version delete")
	}

	if out, err := exec.CommandContext(ctx, deleteJBrowseVersionScript, asm.AssemblyID()).CombinedOutput(); err != nil {
		log.Ctx(ctx).Warn().Err(err).Str("assemblyID", asm.AssemblyID()).Str("scriptOutput", string(out)).
			Msg("failed to clean up JBrowse2 data during assembly version delete")
	}

	for _, f := range files {
		if err := os.Remove(f.FilePath); err != nil && !os.IsNotExist(err) {
			log.Ctx(ctx).Warn().Err(err).Str("path", f.FilePath).Msg("failed to remove upload file from disk during assembly version delete")
		}
	}

	assemblyDir := filepath.Join(uc.uploadDir, v.Name, asm.AssemblyID())
	if err := os.RemoveAll(assemblyDir); err != nil {
		log.Ctx(ctx).Warn().Err(err).Str("path", assemblyDir).Msg("failed to remove assembly version directory during delete")
	}

	return nil
}
