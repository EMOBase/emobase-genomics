package version

import (
	"context"
	"errors"

	"github.com/EMOBase/emobase-genomics/internal/pkg/auth"
	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

var ErrVersionAlreadyExists = errors.New("version already exists")

type UseCase struct {
	versionRepo IVersionRepository
}

func New(versionRepo IVersionRepository) *UseCase {
	return &UseCase{versionRepo: versionRepo}
}

func (uc *UseCase) CreateVersion(ctx context.Context, name string) (*entity.Version, error) {
	existing, err := uc.versionRepo.FindByName(ctx, name)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, ErrVersionAlreadyExists
	}

	v := &entity.Version{
		Name:      name,
		Status:    entity.VersionStatusDraft,
		CreatedBy: auth.UsernameFromContext(ctx),
		UpdatedBy: auth.UsernameFromContext(ctx),
	}

	if err := uc.versionRepo.Create(ctx, v); err != nil {
		return nil, err
	}

	return v, nil
}
