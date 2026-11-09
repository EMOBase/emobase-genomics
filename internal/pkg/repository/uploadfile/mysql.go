package uploadfile

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

type MySQLRepository struct {
	db *sql.DB
}

func New(db *sql.DB) *MySQLRepository {
	return &MySQLRepository{db: db}
}

// notBundleSQL excludes bundle archive rows. Once extracted, every file in a
// bundle has its own row, so the archive row is an audit record only and must
// not show up in file listings or be counted twice in version sizes.
// Append notBundleArgs() to the query args, in the position of this fragment.
var notBundleSQL = ` AND file_type NOT IN (?` + strings.Repeat(",?", len(entity.BundleFileTypes)-1) + `)`

func notBundleArgs() []any {
	args := make([]any, len(entity.BundleFileTypes))
	for i, t := range entity.BundleFileTypes {
		args[i] = t
	}
	return args
}

func (r *MySQLRepository) Create(ctx context.Context, f *entity.UploadFile) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO upload_files (id, version_id, assembly_version_id, file_path, file_type, file_size, metadata, upload_status, created_by)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		f.ID, f.VersionID, f.AssemblyVersionID, f.FilePath, f.FileType, f.FileSize, f.Metadata, f.UploadStatus, f.CreatedBy,
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
		 WHERE version_id IN (`+string(placeholders)+`) AND deleted_at IS NULL`+notBundleSQL+`
		 GROUP BY version_id`,
		append(args, notBundleArgs()...)...,
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
		`SELECT id, version_id, assembly_version_id, file_path, file_type, file_size, metadata, upload_status,
		        created_at, created_by, completed_at, deleted_at, deleted_by
		 FROM upload_files WHERE id = ? AND deleted_at IS NULL`,
		id,
	).Scan(
		&f.ID, &f.VersionID, &f.AssemblyVersionID, &f.FilePath, &f.FileType, &f.FileSize, &f.Metadata, &f.UploadStatus,
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
		`SELECT id, version_id, file_path, file_type, file_size, metadata, upload_status,
		        created_at, created_by, completed_at, deleted_at, deleted_by
		 FROM upload_files WHERE version_id = ? AND deleted_at IS NULL`+notBundleSQL+` ORDER BY created_at DESC`,
		append([]any{versionID}, notBundleArgs()...)...,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var files []entity.UploadFile
	for rows.Next() {
		var f entity.UploadFile
		if err := rows.Scan(
			&f.ID, &f.VersionID, &f.FilePath, &f.FileType, &f.FileSize, &f.Metadata, &f.UploadStatus,
			&f.CreatedAt, &f.CreatedBy, &f.CompletedAt, &f.DeletedAt, &f.DeletedBy,
		); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

// ListByVersionIDAndType returns one page of a version's upload files of a
// single type, newest first.
func (r *MySQLRepository) ListByVersionIDAndType(ctx context.Context, versionID uint64, fileType string, offset, limit int) ([]entity.UploadFile, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, version_id, file_path, file_type, file_size, metadata, upload_status,
		        created_at, created_by, completed_at, deleted_at, deleted_by
		 FROM upload_files WHERE version_id = ? AND file_type = ? AND deleted_at IS NULL
		 ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`,
		versionID, fileType, limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var files []entity.UploadFile
	for rows.Next() {
		var f entity.UploadFile
		if err := rows.Scan(
			&f.ID, &f.VersionID, &f.FilePath, &f.FileType, &f.FileSize, &f.Metadata, &f.UploadStatus,
			&f.CreatedAt, &f.CreatedBy, &f.CompletedAt, &f.DeletedAt, &f.DeletedBy,
		); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

func (r *MySQLRepository) CountByVersionIDAndType(ctx context.Context, versionID uint64, fileType string) (int, error) {
	var total int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM upload_files WHERE version_id = ? AND file_type = ? AND deleted_at IS NULL`,
		versionID, fileType,
	).Scan(&total)
	return total, err
}

// ListByAssemblyVersionID returns every upload file belonging to a single
// Assembly Version — the per-species counterpart of ListByVersionID. Files
// shared across a Database Version rather than owned by one assembly (e.g.
// orthology.tsv) never have an assembly_version_id and so never appear here.
func (r *MySQLRepository) ListByAssemblyVersionID(ctx context.Context, assemblyVersionID uint64) ([]entity.UploadFile, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, version_id, assembly_version_id, file_path, file_type, file_size, metadata, upload_status,
		        created_at, created_by, completed_at, deleted_at, deleted_by
		 FROM upload_files WHERE assembly_version_id = ? AND deleted_at IS NULL ORDER BY created_at DESC`,
		assemblyVersionID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var files []entity.UploadFile
	for rows.Next() {
		var f entity.UploadFile
		if err := rows.Scan(
			&f.ID, &f.VersionID, &f.AssemblyVersionID, &f.FilePath, &f.FileType, &f.FileSize, &f.Metadata, &f.UploadStatus,
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

// ListCurrentCompletedSharedByVersionID returns the Database Version's
// completed files that belong to no assembly and still describe it, the ones a
// release checks: every orthology.tsv and the latest orthology.bundle, the same
// rule as the job repo's StatusCountsForSharedJobs.
func (r *MySQLRepository) ListCurrentCompletedSharedByVersionID(ctx context.Context, versionID uint64) ([]entity.UploadFile, error) {
	args := []any{versionID, entity.UploadStatusCompleted}
	for _, t := range entity.LatestFileTypes {
		args = append(args, t)
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, version_id, file_path, file_type, file_size, metadata, upload_status,
		        created_at, created_by, completed_at, deleted_at, deleted_by
		 FROM (
		   SELECT id, version_id, file_path, file_type, file_size, metadata, upload_status,
		          created_at, created_by, completed_at, deleted_at, deleted_by,
		          ROW_NUMBER() OVER (PARTITION BY file_type ORDER BY created_at DESC, id DESC) AS rn
		   FROM upload_files
		   WHERE version_id = ? AND assembly_version_id IS NULL AND upload_status = ? AND deleted_at IS NULL
		 ) ranked
		 WHERE rn = 1 OR file_type NOT IN (?`+strings.Repeat(",?", len(entity.LatestFileTypes)-1)+`)`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var files []entity.UploadFile
	for rows.Next() {
		var f entity.UploadFile
		if err := rows.Scan(
			&f.ID, &f.VersionID, &f.FilePath, &f.FileType, &f.FileSize, &f.Metadata, &f.UploadStatus,
			&f.CreatedAt, &f.CreatedBy, &f.CompletedAt, &f.DeletedAt, &f.DeletedBy,
		); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

// FindLatestCompletedByVersionAndType returns the most recently created
// COMPLETED file of the given type for the version, or nil if none exists.
func (r *MySQLRepository) FindLatestCompletedByVersionAndType(ctx context.Context, versionID uint64, fileType string) (*entity.UploadFile, error) {
	f := &entity.UploadFile{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, version_id, file_path, file_type, file_size, metadata, upload_status,
		        created_at, created_by, completed_at, deleted_at, deleted_by
		 FROM upload_files
		 WHERE version_id = ? AND file_type = ? AND upload_status = ? AND deleted_at IS NULL
		 ORDER BY created_at DESC LIMIT 1`,
		versionID, fileType, entity.UploadStatusCompleted,
	).Scan(
		&f.ID, &f.VersionID, &f.FilePath, &f.FileType, &f.FileSize, &f.Metadata, &f.UploadStatus,
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

// ListCurrentCompletedByAssemblyVersionID returns the assembly's completed
// files that still describe it, the ones a release checks: the latest of each
// entity.LatestFileTypes type and every file of the other types, the same rule
// as the job repo's StatusCountsByAssemblyVersionID.
func (r *MySQLRepository) ListCurrentCompletedByAssemblyVersionID(ctx context.Context, assemblyVersionID uint64) ([]entity.UploadFile, error) {
	args := []any{assemblyVersionID, entity.UploadStatusCompleted}
	for _, t := range entity.LatestFileTypes {
		args = append(args, t)
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, version_id, assembly_version_id, file_path, file_type, file_size, metadata, upload_status,
		        created_at, created_by, completed_at, deleted_at, deleted_by
		 FROM (
		   SELECT id, version_id, assembly_version_id, file_path, file_type, file_size, metadata, upload_status,
		          created_at, created_by, completed_at, deleted_at, deleted_by,
		          ROW_NUMBER() OVER (PARTITION BY file_type ORDER BY created_at DESC) AS rn
		   FROM upload_files
		   WHERE assembly_version_id = ? AND upload_status = ? AND deleted_at IS NULL
		 ) ranked
		 WHERE rn = 1 OR file_type NOT IN (?`+strings.Repeat(",?", len(entity.LatestFileTypes)-1)+`)`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var files []entity.UploadFile
	for rows.Next() {
		var f entity.UploadFile
		if err := rows.Scan(
			&f.ID, &f.VersionID, &f.AssemblyVersionID, &f.FilePath, &f.FileType, &f.FileSize, &f.Metadata, &f.UploadStatus,
			&f.CreatedAt, &f.CreatedBy, &f.CompletedAt, &f.DeletedAt, &f.DeletedBy,
		); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

// FindLatestCompletedByAssemblyVersionAndType is the per-assembly counterpart
// of FindLatestCompletedByVersionAndType, used to look up e.g. "the" genomic.fna
// for one assembly rather than for the whole Database Version.
func (r *MySQLRepository) FindLatestCompletedByAssemblyVersionAndType(ctx context.Context, assemblyVersionID uint64, fileType string) (*entity.UploadFile, error) {
	f := &entity.UploadFile{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, version_id, assembly_version_id, file_path, file_type, file_size, metadata, upload_status,
		        created_at, created_by, completed_at, deleted_at, deleted_by
		 FROM upload_files
		 WHERE assembly_version_id = ? AND file_type = ? AND upload_status = ? AND deleted_at IS NULL
		 ORDER BY created_at DESC LIMIT 1`,
		assemblyVersionID, fileType, entity.UploadStatusCompleted,
	).Scan(
		&f.ID, &f.VersionID, &f.AssemblyVersionID, &f.FilePath, &f.FileType, &f.FileSize, &f.Metadata, &f.UploadStatus,
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

// ListIDsReplacedBy returns the IDs of the files that fileID replaces: its
// Assembly Version's files of the same type created before it, soft-deleted
// ones included, since their indexed documents may still be there.
func (r *MySQLRepository) ListIDsReplacedBy(ctx context.Context, fileID string) ([]string, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT o.id FROM upload_files o JOIN upload_files f ON f.id = ?
		 WHERE o.assembly_version_id = f.assembly_version_id AND o.file_type = f.file_type
		   AND o.created_at < f.created_at`,
		fileID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *MySQLRepository) SoftDelete(ctx context.Context, id string, deletedBy string) error {
	now := time.Now().UTC()
	_, err := r.db.ExecContext(ctx,
		`UPDATE upload_files SET deleted_at = ?, deleted_by = ? WHERE id = ? AND deleted_at IS NULL`,
		now, deletedBy, id,
	)
	return err
}

func (r *MySQLRepository) HardDeleteByVersionID(ctx context.Context, versionID uint64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM upload_files WHERE version_id = ?`, versionID)
	return err
}

// HardDeleteByAssemblyVersionID is the per-assembly counterpart of
// HardDeleteByVersionID, used when deleting a single Assembly Version rather
// than the whole Database Version.
func (r *MySQLRepository) HardDeleteByAssemblyVersionID(ctx context.Context, assemblyVersionID uint64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM upload_files WHERE assembly_version_id = ?`, assemblyVersionID)
	return err
}
