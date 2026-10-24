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
		`INSERT INTO assembly_versions (version_id, name, species_name, species_code, image_path, info_url, created_by, updated_by)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		a.VersionID, a.Name, a.SpeciesName, a.SpeciesCode, a.ImagePath, a.InfoURL, a.CreatedBy, a.UpdatedBy,
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
		`SELECT id, version_id, name, species_name, species_code, image_path, info_url, created_at, created_by, updated_at, updated_by
		 FROM assembly_versions WHERE id = ?`,
		id,
	).Scan(
		&a.ID, &a.VersionID, &a.Name, &a.SpeciesName, &a.SpeciesCode, &a.ImagePath, &a.InfoURL,
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
		`SELECT id, version_id, name, species_name, species_code, image_path, info_url, created_at, created_by, updated_at, updated_by
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
			&a.ID, &a.VersionID, &a.Name, &a.SpeciesName, &a.SpeciesCode, &a.ImagePath, &a.InfoURL,
			&a.CreatedAt, &a.CreatedBy, &a.UpdatedAt, &a.UpdatedBy,
		); err != nil {
			return nil, err
		}
		assemblies = append(assemblies, a)
	}
	return assemblies, rows.Err()
}

// Update writes the fields PATCH may change. The species fields are left out:
// the code is copied into indexed documents at upload time, so they never change.
func (r *MySQLRepository) Update(ctx context.Context, a *entity.AssemblyVersion) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE assembly_versions SET name = ?, info_url = ?, updated_by = ? WHERE id = ?`,
		a.Name, a.InfoURL, a.UpdatedBy, a.ID,
	)
	return err
}

// UpdateImagePath sets the assembly's image, or clears it when imagePath is nil.
func (r *MySQLRepository) UpdateImagePath(ctx context.Context, id uint64, imagePath *string, updatedBy string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE assembly_versions SET image_path = ?, updated_by = ? WHERE id = ?`,
		imagePath, updatedBy, id,
	)
	return err
}

func (r *MySQLRepository) Delete(ctx context.Context, id uint64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM assembly_versions WHERE id = ?`, id)
	return err
}

// DeleteByVersionID deletes every Assembly Version under a Database Version —
// used when deleting the whole Database Version. Callers must delete any
// referencing jobs/upload_files rows first (FK constraint).
func (r *MySQLRepository) DeleteByVersionID(ctx context.Context, versionID uint64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM assembly_versions WHERE version_id = ?`, versionID)
	return err
}
