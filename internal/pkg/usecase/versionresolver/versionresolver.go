package versionresolver

import (
	"context"
	"errors"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

var (
	ErrVersionNotFound  = errors.New("version not found")
	ErrNoDefaultVersion = errors.New("no default version configured")
)

type IVersionRepository interface {
	FindByName(ctx context.Context, name string) (*entity.Version, error)
	FindByID(ctx context.Context, id uint64) (*entity.Version, error)
}

type IAppSettingsRepository interface {
	GetDefaultVersionID(ctx context.Context) (*uint64, error)
}

// Resolver resolves a version name (or empty string for the default) to an entity.Version.
type Resolver struct {
	versionRepo     IVersionRepository
	appSettingsRepo IAppSettingsRepository
}

func New(versionRepo IVersionRepository, appSettingsRepo IAppSettingsRepository) Resolver {
	return Resolver{versionRepo: versionRepo, appSettingsRepo: appSettingsRepo}
}

// Resolve returns the version for the given name, or the default version if name is empty.
// Returns ErrVersionNotFound or ErrNoDefaultVersion on failure.
func (r Resolver) Resolve(ctx context.Context, versionName string) (*entity.Version, error) {
	if versionName != "" {
		v, err := r.versionRepo.FindByName(ctx, versionName)
		if err != nil {
			return nil, err
		}
		if v == nil {
			return nil, ErrVersionNotFound
		}
		return v, nil
	}

	defaultID, err := r.appSettingsRepo.GetDefaultVersionID(ctx)
	if err != nil {
		return nil, err
	}
	if defaultID == nil {
		return nil, ErrNoDefaultVersion
	}

	v, err := r.versionRepo.FindByID(ctx, *defaultID)
	if err != nil {
		return nil, err
	}
	if v == nil {
		return nil, ErrNoDefaultVersion
	}
	return v, nil
}
