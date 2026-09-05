package assemblyversion

import (
	"context"
	"database/sql"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

type MySQLRepository struct {
	db *sql.DB
}

func New(db *sql.DB) *MySQLRepository {
	return &MySQLRepository{db: db}
}

func (r *MySQLRepository) Create(ctx context.Context, a *entity.AssemblyVersion) error {
	result, err := r.db.ExecContext(ctx,
		`INSERT INTO assembly_versions (version_id, name, species, created_by, updated_by)
		 VALUES (?, ?, ?, ?, ?)`,
		a.VersionID, a.Name, a.Species, a.CreatedBy, a.UpdatedBy,
	)
	if err != nil {
		return err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	a.ID = uint64(id)

	return r.db.QueryRowContext(ctx,
		`SELECT created_at, updated_at FROM assembly_versions WHERE id = ?`, a.ID,
	).Scan(&a.CreatedAt, &a.UpdatedAt)
}

func (r *MySQLRepository) FindByID(ctx context.Context, id uint64) (*entity.AssemblyVersion, error) {
	a := &entity.AssemblyVersion{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, version_id, name, species, created_at, created_by, updated_at, updated_by
		 FROM assembly_versions WHERE id = ?`,
		id,
	).Scan(
		&a.ID, &a.VersionID, &a.Name, &a.Species,
		&a.CreatedAt, &a.CreatedBy, &a.UpdatedAt, &a.UpdatedBy,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return a, nil
}

// FindBySpecies looks up an Assembly Version by its species code, scoped to a
// single Database Version — the primary lookup used to resolve the upload
// `assembly` metadata field and the `:species` API path parameter, mirroring
// repository/version's FindByName.
func (r *MySQLRepository) FindBySpecies(ctx context.Context, versionID uint64, species string) (*entity.AssemblyVersion, error) {
	a := &entity.AssemblyVersion{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, version_id, name, species, created_at, created_by, updated_at, updated_by
		 FROM assembly_versions WHERE version_id = ? AND species = ?`,
		versionID, species,
	).Scan(
		&a.ID, &a.VersionID, &a.Name, &a.Species,
		&a.CreatedAt, &a.CreatedBy, &a.UpdatedAt, &a.UpdatedBy,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return a, nil
}

// ListByVersionID returns every Assembly Version under a Database Version,
// ordered by creation time.
func (r *MySQLRepository) ListByVersionID(ctx context.Context, versionID uint64) ([]entity.AssemblyVersion, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, version_id, name, species, created_at, created_by, updated_at, updated_by
		 FROM assembly_versions WHERE version_id = ? ORDER BY created_at ASC`,
		versionID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var assemblies []entity.AssemblyVersion
	for rows.Next() {
		var a entity.AssemblyVersion
		if err := rows.Scan(
			&a.ID, &a.VersionID, &a.Name, &a.Species,
			&a.CreatedAt, &a.CreatedBy, &a.UpdatedAt, &a.UpdatedBy,
		); err != nil {
			return nil, err
		}
		assemblies = append(assemblies, a)
	}
	return assemblies, rows.Err()
}

func (r *MySQLRepository) Delete(ctx context.Context, id uint64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM assembly_versions WHERE id = ?`, id)
	return err
}
