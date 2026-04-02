package uploadfile

import (
	"context"
	"database/sql"
	"time"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

type MySQLRepository struct {
	db *sql.DB
}

func New(db *sql.DB) *MySQLRepository {
	return &MySQLRepository{db: db}
}

func (r *MySQLRepository) Create(ctx context.Context, f *entity.UploadFile) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO upload_files (id, version_id, file_path, file_type, metadata, upload_status, created_by)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		f.ID, f.VersionID, f.FilePath, f.FileType, f.Metadata, f.UploadStatus, f.CreatedBy,
	)
	return err
}

func (r *MySQLRepository) FindByID(ctx context.Context, id string) (*entity.UploadFile, error) {
	f := &entity.UploadFile{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, version_id, file_path, file_type, metadata, upload_status,
		        created_at, created_by, completed_at, deleted_at, deleted_by
		 FROM upload_files WHERE id = ? AND deleted_at IS NULL`,
		id,
	).Scan(
		&f.ID, &f.VersionID, &f.FilePath, &f.FileType, &f.Metadata, &f.UploadStatus,
		&f.CreatedAt, &f.CreatedBy, &f.CompletedAt, &f.DeletedAt, &f.DeletedBy,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (r *MySQLRepository) ListByVersionID(ctx context.Context, versionID uint64) ([]entity.UploadFile, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, version_id, file_path, file_type, metadata, upload_status,
		        created_at, created_by, completed_at, deleted_at, deleted_by
		 FROM upload_files WHERE version_id = ? AND deleted_at IS NULL ORDER BY created_at DESC`,
		versionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []entity.UploadFile
	for rows.Next() {
		var f entity.UploadFile
		if err := rows.Scan(
			&f.ID, &f.VersionID, &f.FilePath, &f.FileType, &f.Metadata, &f.UploadStatus,
			&f.CreatedAt, &f.CreatedBy, &f.CompletedAt, &f.DeletedAt, &f.DeletedBy,
		); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

func (r *MySQLRepository) UpdateStatus(ctx context.Context, id string, status entity.UploadStatus) error {
	var completedAt *time.Time
	if status == entity.UploadStatusCompleted || status == entity.UploadStatusFailed {
		now := time.Now().UTC()
		completedAt = &now
	}
	_, err := r.db.ExecContext(ctx,
		`UPDATE upload_files SET upload_status = ?, completed_at = ? WHERE id = ? AND deleted_at IS NULL`,
		status, completedAt, id,
	)
	return err
}

func (r *MySQLRepository) SoftDelete(ctx context.Context, id string, deletedBy string) error {
	now := time.Now().UTC()
	_, err := r.db.ExecContext(ctx,
		`UPDATE upload_files SET deleted_at = ?, deleted_by = ? WHERE id = ? AND deleted_at IS NULL`,
		now, deletedBy, id,
	)
	return err
}
