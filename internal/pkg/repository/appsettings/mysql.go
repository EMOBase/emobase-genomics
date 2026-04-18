package appsettings

import (
	"context"
	"database/sql"
)

type MySQLRepository struct {
	db *sql.DB
}

func New(db *sql.DB) *MySQLRepository {
	return &MySQLRepository{db: db}
}

// SetDefaultVersion updates the global default version ID in app_settings.
func (r *MySQLRepository) SetDefaultVersion(ctx context.Context, versionID uint64) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE app_settings SET default_version_id = ? LIMIT 1`,
		versionID,
	)
	return err
}

// GetDefaultVersionID returns the current default_version_id, or nil if unset.
func (r *MySQLRepository) GetDefaultVersionID(ctx context.Context) (*uint64, error) {
	var id *uint64
	err := r.db.QueryRowContext(ctx,
		`SELECT default_version_id FROM app_settings LIMIT 1`,
	).Scan(&id)
	return id, err
}
