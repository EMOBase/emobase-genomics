package assemblyversion

import (
	"context"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	ucversion "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/version"
)

type IVersionRepository interface {
	FindByName(ctx context.Context, name string) (*entity.Version, error)
}

type IAssemblyVersionRepository interface {
	Create(ctx context.Context, a *entity.AssemblyVersion) error
	FindBySpecies(ctx context.Context, versionID uint64, species string) (*entity.AssemblyVersion, error)
	ListByVersionID(ctx context.Context, versionID uint64) ([]entity.AssemblyVersion, error)
	Delete(ctx context.Context, id uint64) error
}

// IJobRepository embeds usecase/version's job repository interface (needed by
// version.BuildAssemblyVersionDetail, which this package reuses for
// GetAssemblyVersionDetail) plus the extra methods DeleteAssemblyVersion needs.
type IJobRepository interface {
	ucversion.IJobRepository
	HasActiveJobsByAssemblyVersionID(ctx context.Context, assemblyVersionID uint64) (bool, error)
	DeleteByAssemblyVersionID(ctx context.Context, assemblyVersionID uint64) error
}

// IUploadFileRepository embeds usecase/version's upload file repository
// interface plus the extra method DeleteAssemblyVersion needs.
type IUploadFileRepository interface {
	ucversion.IUploadFileRepository
	HardDeleteByAssemblyVersionID(ctx context.Context, assemblyVersionID uint64) error
}
