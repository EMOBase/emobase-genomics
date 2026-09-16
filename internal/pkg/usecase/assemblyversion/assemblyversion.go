package assemblyversion

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"

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
	ErrAssemblyVersionAlreadyExists = errors.New("assembly version already exists")
	ErrAssemblyVersionNotFound      = errors.New("assembly version not found")
	ErrAssemblyVersionHasActiveJobs = errors.New("assembly version has active jobs")
)

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

// CreateAssemblyVersion creates a new species within a Database Version.
// Every Assembly Version is created identically — there is no auto-assigned
// "primary"/"default" one (§2 of the design doc).
func (uc *UseCase) CreateAssemblyVersion(ctx context.Context, versionName, name, species string) (*entity.AssemblyVersion, error) {
	v, err := uc.versionRepo.FindByName(ctx, versionName)
	if err != nil {
		return nil, err
	}
	if v == nil {
		return nil, ErrVersionNotFound
	}

	existing, err := uc.assemblyVersionRepo.FindBySpecies(ctx, v.ID, species)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, ErrAssemblyVersionAlreadyExists
	}

	a := &entity.AssemblyVersion{
		VersionID: v.ID,
		Name:      name,
		Species:   species,
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
func (uc *UseCase) GetAssemblyVersionDetail(ctx context.Context, versionName, species string) (*ucversion.AssemblyVersionDetail, error) {
	v, err := uc.versionRepo.FindByName(ctx, versionName)
	if err != nil {
		return nil, err
	}
	if v == nil {
		return nil, ErrVersionNotFound
	}

	asm, err := uc.assemblyVersionRepo.FindBySpecies(ctx, v.ID, species)
	if err != nil {
		return nil, err
	}
	if asm == nil {
		return nil, ErrAssemblyVersionNotFound
	}

	return ucversion.BuildAssemblyVersionDetail(ctx, uc.jobRepo, uc.uploadFileRepo, *asm)
}

// DeleteAssemblyVersion deletes one species from a Database Version: its
// jobs, upload file rows and on-disk files, ES indexes, its JBrowse2
// assembly/tracks, and the assembly_versions row itself. There is no
// orthology-related guard — deleting an assembly never touches the Database
// Version's shared orthology.tsv data, since orthology belongs to no single
// assembly (§2 of the design doc).
func (uc *UseCase) DeleteAssemblyVersion(ctx context.Context, versionName, species string) error {
	v, err := uc.versionRepo.FindByName(ctx, versionName)
	if err != nil {
		return err
	}
	if v == nil {
		return ErrVersionNotFound
	}

	asm, err := uc.assemblyVersionRepo.FindBySpecies(ctx, v.ID, species)
	if err != nil {
		return err
	}
	if asm == nil {
		return ErrAssemblyVersionNotFound
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

	if err := uc.esRepo.DeleteIndexesByAssemblyVersion(ctx, v.Name, asm.Species); err != nil {
		log.Ctx(ctx).Warn().Err(err).Str("version", v.Name).Str("species", asm.Species).Msg("failed to delete ES indexes during assembly version delete")
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

	assemblyDir := filepath.Join(uc.uploadDir, v.Name, asm.Species)
	if err := os.RemoveAll(assemblyDir); err != nil {
		log.Ctx(ctx).Warn().Err(err).Str("path", assemblyDir).Msg("failed to remove assembly version directory during delete")
	}

	return nil
}
