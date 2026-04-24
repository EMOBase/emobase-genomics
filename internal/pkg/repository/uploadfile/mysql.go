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
		`INSERT INTO upload_files (id, version_id, file_path, file_type, file_size, metadata, upload_status, created_by)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		f.ID, f.VersionID, f.FilePath, f.FileType, f.FileSize, f.Metadata, f.UploadStatus, f.CreatedBy,
	)
	return err
}

func (r *MySQLRepository) TotalFileSizeByVersionIDs(ctx context.Context, versionIDs []uint64) (map[uint64]int64, error) {
	if len(versionIDs) == 0 {
		return map[uint64]int64{}, nil
	}

	placeholders := make([]byte, 0, len(versionIDs)*2-1)
	args := make([]any, len(versionIDs))
	for i, id := range versionIDs {
		if i > 0 {
			placeholders = append(placeholders, ',')
		}
		placeholders = append(placeholders, '?')
		args[i] = id
	}

	rows, err := r.db.QueryContext(ctx,
		`SELECT version_id, COALESCE(SUM(file_size), 0)
		 FROM upload_files
		 WHERE version_id IN (`+string(placeholders)+`) AND deleted_at IS NULL
		 GROUP BY version_id`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	result := make(map[uint64]int64, len(versionIDs))
	for rows.Next() {
		var versionID uint64
		var total int64
		if err := rows.Scan(&versionID, &total); err != nil {
			return nil, err
		}
		result[versionID] = total
	}
	return result, rows.Err()
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
	defer func() { _ = rows.Close() }()

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

// FindLatestCompletedByVersionAndType returns the most recently created
// COMPLETED file of the given type for the version, or nil if none exists.
func (r *MySQLRepository) FindLatestCompletedByVersionAndType(ctx context.Context, versionID uint64, fileType string) (*entity.UploadFile, error) {
	f := &entity.UploadFile{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, version_id, file_path, file_type, metadata, upload_status,
		        created_at, created_by, completed_at, deleted_at, deleted_by
		 FROM upload_files
		 WHERE version_id = ? AND file_type = ? AND upload_status = ? AND deleted_at IS NULL
		 ORDER BY created_at DESC LIMIT 1`,
		versionID, fileType, entity.UploadStatusCompleted,
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

func (r *MySQLRepository) SoftDelete(ctx context.Context, id string, deletedBy string) error {
	now := time.Now().UTC()
	_, err := r.db.ExecContext(ctx,
		`UPDATE upload_files SET deleted_at = ?, deleted_by = ? WHERE id = ? AND deleted_at IS NULL`,
		now, deletedBy, id,
	)
	return err
}
