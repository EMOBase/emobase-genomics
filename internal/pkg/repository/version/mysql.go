package version

import (
	"context"
	"database/sql"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

type MySQLRepository struct {
	db *sql.DB
}

func NewMySQLRepository(db *sql.DB) *MySQLRepository {
	return &MySQLRepository{db: db}
}

func (r *MySQLRepository) Create(ctx context.Context, v *entity.Version) error {
	result, err := r.db.ExecContext(ctx,
		`INSERT INTO versions (name, description, status, created_by, updated_by)
		 VALUES (?, ?, ?, ?, ?)`,
		v.Name, v.Description, v.Status, v.CreatedBy, v.UpdatedBy,
	)
	if err != nil {
		return err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	v.ID = uint64(id)
	return nil
}

func (r *MySQLRepository) FindByID(ctx context.Context, id uint64) (*entity.Version, error) {
	v := &entity.Version{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, description, status, created_at, created_by, updated_at, updated_by
		 FROM versions WHERE id = ?`,
		id,
	).Scan(
		&v.ID, &v.Name, &v.Description, &v.Status,
		&v.CreatedAt, &v.CreatedBy, &v.UpdatedAt, &v.UpdatedBy,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return v, nil
}

func (r *MySQLRepository) List(ctx context.Context) ([]entity.Version, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, description, status, created_at, created_by, updated_at, updated_by
		 FROM versions ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var versions []entity.Version
	for rows.Next() {
		var v entity.Version
		if err := rows.Scan(
			&v.ID, &v.Name, &v.Description, &v.Status,
			&v.CreatedAt, &v.CreatedBy, &v.UpdatedAt, &v.UpdatedBy,
		); err != nil {
			return nil, err
		}
		versions = append(versions, v)
	}
	return versions, rows.Err()
}

func (r *MySQLRepository) UpdateStatus(ctx context.Context, id uint64, status entity.VersionStatus, updatedBy string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE versions SET status = ?, updated_by = ? WHERE id = ?`,
		status, updatedBy, id,
	)
	return err
}

func (r *MySQLRepository) Delete(ctx context.Context, id uint64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM versions WHERE id = ?`, id)
	return err
}
