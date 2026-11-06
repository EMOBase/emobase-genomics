package job

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

func (r *MySQLRepository) Create(ctx context.Context, j *entity.Job) error {
	result, err := r.db.ExecContext(ctx,
		`INSERT INTO jobs (version_id, assembly_version_id, file_id, type, description, payload, status)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		j.VersionID, j.AssemblyVersionID, j.FileID, j.Type, j.Description, j.Payload, j.Status,
	)
	if err != nil {
		return err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	j.ID = uint64(id)
	return nil
}

// FindByID returns the job with the given ID, or nil if none exists.
func (r *MySQLRepository) FindByID(ctx context.Context, id uint64) (*entity.Job, error) {
	var j entity.Job
	err := r.db.QueryRowContext(ctx,
		`SELECT id, version_id, assembly_version_id, file_id, type, description, payload, status, result_metadata,
		        created_at, updated_at, started_at, completed_at
		 FROM jobs WHERE id = ?`,
		id,
	).Scan(
		&j.ID, &j.VersionID, &j.AssemblyVersionID, &j.FileID, &j.Type, &j.Description, &j.Payload, &j.Status, &j.ResultMetadata,
		&j.CreatedAt, &j.UpdatedAt, &j.StartedAt, &j.CompletedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &j, nil
}

// FindByVersionID returns all jobs for the given version ID, ordered by creation time.
func (r *MySQLRepository) FindByVersionID(ctx context.Context, versionID uint64) ([]entity.Job, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, version_id, file_id, type, description, status, payload, result_metadata
		 FROM jobs WHERE version_id = ?
		 ORDER BY created_at ASC`,
		versionID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var jobs []entity.Job
	for rows.Next() {
		var j entity.Job
		if err := rows.Scan(&j.ID, &j.VersionID, &j.FileID, &j.Type, &j.Description, &j.Status, &j.Payload, &j.ResultMetadata); err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

// FindByFileIDs returns all jobs for the given upload files, ordered by
// creation time.
func (r *MySQLRepository) FindByFileIDs(ctx context.Context, fileIDs []string) ([]entity.Job, error) {
	if len(fileIDs) == 0 {
		return nil, nil
	}

	args := make([]any, len(fileIDs))
	for i, id := range fileIDs {
		args[i] = id
	}

	rows, err := r.db.QueryContext(ctx,
		`SELECT id, version_id, file_id, type, description, status, payload, result_metadata
		 FROM jobs WHERE file_id IN (?`+strings.Repeat(",?", len(fileIDs)-1)+`)
		 ORDER BY created_at ASC`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var jobs []entity.Job
	for rows.Next() {
		var j entity.Job
		if err := rows.Scan(&j.ID, &j.VersionID, &j.FileID, &j.Type, &j.Description, &j.Status, &j.Payload, &j.ResultMetadata); err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

// FindByAssemblyVersionID returns all jobs for the given Assembly Version,
// ordered by creation time — the per-species counterpart of FindByVersionID.
// Jobs for file types shared across a Database Version rather than owned by
// one assembly (e.g. ORTHOLOGY.TSV) never have an assembly_version_id and so
// never appear here.
func (r *MySQLRepository) FindByAssemblyVersionID(ctx context.Context, assemblyVersionID uint64) ([]entity.Job, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, version_id, assembly_version_id, file_id, type, description, status, payload, result_metadata
		 FROM jobs WHERE assembly_version_id = ?
		 ORDER BY created_at ASC`,
		assemblyVersionID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var jobs []entity.Job
	for rows.Next() {
		var j entity.Job
		if err := rows.Scan(&j.ID, &j.VersionID, &j.AssemblyVersionID, &j.FileID, &j.Type, &j.Description, &j.Status, &j.Payload, &j.ResultMetadata); err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

// ClaimNextPending atomically selects the oldest PENDING job and marks it
// RUNNING, using FOR UPDATE SKIP LOCKED so concurrent workers never claim the
// same job.
func (r *MySQLRepository) ClaimNextPending(ctx context.Context) (*entity.Job, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	j := &entity.Job{}
	err = tx.QueryRowContext(ctx,
		`SELECT id, version_id, assembly_version_id, file_id, type, description, payload, status,
		        result_metadata, created_at, updated_at, started_at, completed_at
		 FROM jobs WHERE status = ?
		 ORDER BY created_at ASC LIMIT 1 FOR UPDATE SKIP LOCKED`,
		entity.JobStatusPending,
	).Scan(
		&j.ID, &j.VersionID, &j.AssemblyVersionID, &j.FileID, &j.Type, &j.Description, &j.Payload, &j.Status,
		&j.ResultMetadata, &j.CreatedAt, &j.UpdatedAt, &j.StartedAt, &j.CompletedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	if _, err = tx.ExecContext(ctx,
		`UPDATE jobs SET status = ?, started_at = ?, updated_at = ? WHERE id = ?`,
		entity.JobStatusRunning, now, now, j.ID,
	); err != nil {
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	j.Status = entity.JobStatusRunning
	j.StartedAt = &now
	j.UpdatedAt = now
	return j, nil
}

// RequeueStuckJobs resets RUNNING jobs whose started_at is before stuckBefore
// back to PENDING, so they can be picked up again.
func (r *MySQLRepository) RequeueStuckJobs(ctx context.Context, stuckBefore time.Time) (int64, error) {
	result, err := r.db.ExecContext(ctx,
		`UPDATE jobs SET status = ?, started_at = NULL, updated_at = NOW()
		 WHERE status = ? AND started_at < ?`,
		entity.JobStatusPending, entity.JobStatusRunning, stuckBefore,
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (r *MySQLRepository) MarkDone(ctx context.Context, id uint64, resultMetadata []byte) error {
	now := time.Now().UTC()
	_, err := r.db.ExecContext(ctx,
		`UPDATE jobs SET status = ?, completed_at = ?, result_metadata = ? WHERE id = ?`,
		entity.JobStatusDone, now, resultMetadata, id,
	)
	return err
}

func (r *MySQLRepository) MarkFailed(ctx context.Context, id uint64, resultMetadata []byte) error {
	now := time.Now().UTC()
	_, err := r.db.ExecContext(ctx,
		`UPDATE jobs SET status = ?, completed_at = ?, result_metadata = ? WHERE id = ?`,
		entity.JobStatusFailed, now, resultMetadata, id,
	)
	return err
}

func (r *MySQLRepository) HasActiveJobOfType(ctx context.Context, versionID uint64, jobType string) (bool, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM jobs
		 WHERE version_id = ? AND type = ? AND status IN (?, ?)`,
		versionID, jobType, entity.JobStatusPending, entity.JobStatusRunning,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// HasActiveJobOfTypeForAssemblyVersion is the per-assembly counterpart of
// HasActiveJobOfType, used to scope upload dedup checks to one species instead
// of the whole Database Version.
func (r *MySQLRepository) HasActiveJobOfTypeForAssemblyVersion(ctx context.Context, assemblyVersionID uint64, jobType string) (bool, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM jobs
		 WHERE assembly_version_id = ? AND type = ? AND status IN (?, ?)`,
		assemblyVersionID, jobType, entity.JobStatusPending, entity.JobStatusRunning,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// HasActiveJobForFileTypeInAssemblyVersion reports whether a PENDING or
// RUNNING job of any type works on one of the Assembly Version's files of
// fileType: the file's processing job, the jobs chained from it (a GFF's
// synonym and JBrowse2 jobs, an FNA's JBrowse2 setup), or a release's BLAST
// setup reading it.
func (r *MySQLRepository) HasActiveJobForFileTypeInAssemblyVersion(ctx context.Context, assemblyVersionID uint64, fileType string) (bool, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM jobs j JOIN upload_files f ON f.id = j.file_id
		 WHERE j.assembly_version_id = ? AND f.file_type = ? AND j.status IN (?, ?)`,
		assemblyVersionID, fileType, entity.JobStatusPending, entity.JobStatusRunning,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *MySQLRepository) HasActiveJobOfTypeForFile(ctx context.Context, fileID string, jobType string) (bool, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM jobs
		 WHERE file_id = ? AND type = ? AND status IN (?, ?)`,
		fileID, jobType, entity.JobStatusPending, entity.JobStatusRunning,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// StatusCountsByAssemblyVersionID returns job status counts used to compute
// one Assembly Version's own status (the Database-Version-level status is then
// a rollup across all of its assemblies, computed in the usecase layer).
// Orthology jobs never carry an assembly_version_id, so they're excluded.
//
// Only jobs that still describe the assembly count, so that a failure can be
// cleared: jobs of the latest file of each type, and of those only the latest
// attempt per file and job type (a re-run replaces an earlier failure). A job
// with no file (e.g. REMOVE_BLAST) counts only if it is the latest of its type.
func (r *MySQLRepository) StatusCountsByAssemblyVersionID(ctx context.Context, assemblyVersionID uint64) (entity.JobStatusCounts, error) {
	var counts entity.JobStatusCounts
	err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(status = ?), 0) AS running_count,
		       COALESCE(SUM(status = ?), 0) AS failed_count,
		       COALESCE(SUM(status = ?), 0) AS done_count,
		       COUNT(*)                      AS total_count
		FROM (
		  SELECT j.status,
		         ROW_NUMBER() OVER (PARTITION BY j.file_id, j.type ORDER BY j.id DESC) AS attempt
		  FROM jobs j
		  WHERE j.assembly_version_id = ?
		    AND (
		      j.file_id IS NULL
		      OR j.file_id IN (
		        SELECT id FROM (
		          SELECT id, file_type,
		                 ROW_NUMBER() OVER (PARTITION BY file_type ORDER BY created_at DESC) AS rn
		          FROM upload_files
		          WHERE assembly_version_id = ? AND deleted_at IS NULL
		        ) ranked
		        WHERE rn = 1
		      )
		    )
		) latest
		WHERE attempt = 1`,
		entity.JobStatusRunning, entity.JobStatusFailed, entity.JobStatusDone,
		assemblyVersionID, assemblyVersionID,
	).Scan(&counts.RunningCount, &counts.FailedCount, &counts.DoneCount, &counts.TotalCount)
	return counts, err
}

// StatusCountsForSharedJobs returns job status counts for a Database
// Version's shared files, those not owned by any single Assembly Version
// (orthology.tsv and orthology.bundle) — used to fold orthology's own status
// into the Database-Version-level status rollup.
//
// Only jobs that still describe the Version count, so that a failure can be
// cleared: jobs of files not deleted, and of those only the latest attempt per
// file and job type. Every orthology.tsv counts, but only the latest
// orthology.bundle: a failed bundle extracts nothing and cannot be deleted, so
// uploading a corrected bundle must clear it. Jobs with no file (e.g. a BLAST
// restart) are not about the shared files and do not count.
func (r *MySQLRepository) StatusCountsForSharedJobs(ctx context.Context, versionID uint64) (entity.JobStatusCounts, error) {
	var counts entity.JobStatusCounts
	err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(status = ?), 0) AS running_count,
		       COALESCE(SUM(status = ?), 0) AS failed_count,
		       COALESCE(SUM(status = ?), 0) AS done_count,
		       COUNT(*)                      AS total_count
		FROM (
		  SELECT j.status,
		         ROW_NUMBER() OVER (PARTITION BY j.file_id, j.type ORDER BY j.id DESC) AS attempt
		  FROM jobs j
		  JOIN upload_files f ON f.id = j.file_id
		  WHERE j.version_id = ? AND j.assembly_version_id IS NULL AND f.deleted_at IS NULL
		    AND (f.file_type <> ? OR f.id = (
		      SELECT b.id FROM upload_files b
		      WHERE b.version_id = ? AND b.file_type = ? AND b.deleted_at IS NULL
		      ORDER BY b.created_at DESC, b.id DESC LIMIT 1
		    ))
		) latest
		WHERE attempt = 1`,
		entity.JobStatusRunning, entity.JobStatusFailed, entity.JobStatusDone,
		versionID, entity.FileTypeOrthologyBundle, versionID, entity.FileTypeOrthologyBundle,
	).Scan(&counts.RunningCount, &counts.FailedCount, &counts.DoneCount, &counts.TotalCount)
	return counts, err
}

// FindDoneByVersionAndTypes returns DONE jobs matching any of the given types
// for the specified version. Used to check prerequisite job completion.
func (r *MySQLRepository) FindDoneByVersionAndTypes(ctx context.Context, versionID uint64, jobTypes []string) ([]entity.Job, error) {
	if len(jobTypes) == 0 {
		return nil, nil
	}

	placeholders := make([]byte, 0, len(jobTypes)*2-1)
	args := make([]any, 0, len(jobTypes)+2)
	args = append(args, versionID, entity.JobStatusDone)
	for i, t := range jobTypes {
		if i > 0 {
			placeholders = append(placeholders, ',')
		}
		placeholders = append(placeholders, '?')
		args = append(args, t)
	}

	rows, err := r.db.QueryContext(ctx,
		`SELECT id, version_id, file_id, type, payload FROM jobs
		 WHERE version_id = ? AND status = ? AND type IN (`+string(placeholders)+`)`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var jobs []entity.Job
	for rows.Next() {
		var j entity.Job
		if err := rows.Scan(&j.ID, &j.VersionID, &j.FileID, &j.Type, &j.Payload); err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

// FindDoneByAssemblyVersionAndTypes returns DONE jobs matching any of the
// given types for the specified Assembly Version. Used to check prerequisite
// job completion scoped to one species, not the whole Database Version.
func (r *MySQLRepository) FindDoneByAssemblyVersionAndTypes(ctx context.Context, assemblyVersionID uint64, jobTypes []string) ([]entity.Job, error) {
	if len(jobTypes) == 0 {
		return nil, nil
	}

	placeholders := make([]byte, 0, len(jobTypes)*2-1)
	args := make([]any, 0, len(jobTypes)+2)
	args = append(args, assemblyVersionID, entity.JobStatusDone)
	for i, t := range jobTypes {
		if i > 0 {
			placeholders = append(placeholders, ',')
		}
		placeholders = append(placeholders, '?')
		args = append(args, t)
	}

	rows, err := r.db.QueryContext(ctx,
		`SELECT id, version_id, file_id, type, payload FROM jobs
		 WHERE assembly_version_id = ? AND status = ? AND type IN (`+string(placeholders)+`)`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var jobs []entity.Job
	for rows.Next() {
		var j entity.Job
		if err := rows.Scan(&j.ID, &j.VersionID, &j.FileID, &j.Type, &j.Payload); err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

// HasInFlightJobOfType returns true if a PENDING or RUNNING job of the given
// type exists for the version. Used to prevent duplicate concurrent
// enqueuing (e.g. a double-clicked release) — deliberately ignores DONE jobs
// so a version can be released again later (e.g. BLAST DB paths are global,
// not per-version, so switching the active default back to an
// already-released version must rebuild them from that version's files
// again, not skip on the grounds that a same-typed job ran before).
func (r *MySQLRepository) HasInFlightJobOfType(ctx context.Context, versionID uint64, jobType string) (bool, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM jobs
		 WHERE version_id = ? AND type = ? AND status IN (?, ?)`,
		versionID, jobType, entity.JobStatusPending, entity.JobStatusRunning,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// HasNonFailedJobOfTypeForFile returns true if a PENDING, RUNNING, or DONE job
// of the given type exists for the specific file. Used to prevent duplicate
// per-file job enqueuing.
func (r *MySQLRepository) HasNonFailedJobOfTypeForFile(ctx context.Context, fileID string, jobType string) (bool, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM jobs
		 WHERE file_id = ? AND type = ? AND status IN (?, ?, ?)`,
		fileID, jobType, entity.JobStatusPending, entity.JobStatusRunning, entity.JobStatusDone,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// HasNonDoneJobsForFile returns true if any job for the given file is not yet
// in the DONE state (i.e. PENDING, RUNNING, or FAILED).
func (r *MySQLRepository) HasNonDoneJobsForFile(ctx context.Context, fileID string) (bool, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM jobs WHERE file_id = ? AND status != ?`,
		fileID, entity.JobStatusDone,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// HasNonDoneJobOfTypesForVersion returns true if any job of the given types for
// the version is not yet DONE (i.e. still PENDING, RUNNING, or FAILED).
func (r *MySQLRepository) HasNonDoneJobOfTypesForVersion(ctx context.Context, versionID uint64, jobTypes []string) (bool, error) {
	if len(jobTypes) == 0 {
		return false, nil
	}
	placeholders := strings.Repeat(",?", len(jobTypes))[1:]
	args := []any{versionID, entity.JobStatusDone}
	for _, t := range jobTypes {
		args = append(args, t)
	}
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM jobs WHERE version_id = ? AND status != ? AND type IN (`+placeholders+`)`,
		args...,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// HasDoneJobOfTypeForFile returns true if a DONE job of the given type exists for
// the specific file. Used to confirm that the current version of a file has
// been fully processed, independent of jobs for other files of the same type.
func (r *MySQLRepository) HasDoneJobOfTypeForFile(ctx context.Context, fileID string, jobType string) (bool, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM jobs WHERE file_id = ? AND type = ? AND status = ?`,
		fileID, jobType, entity.JobStatusDone,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// FindLatestByFileAndType returns the most recently created job of the given
// type for a specific file, or nil if none exists.
func (r *MySQLRepository) FindLatestByFileAndType(ctx context.Context, fileID string, jobType string) (*entity.Job, error) {
	var j entity.Job
	err := r.db.QueryRowContext(ctx,
		`SELECT id, version_id, file_id, type, payload
		 FROM jobs WHERE file_id = ? AND type = ? ORDER BY id DESC LIMIT 1`,
		fileID, jobType,
	).Scan(&j.ID, &j.VersionID, &j.FileID, &j.Type, &j.Payload)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &j, nil
}

func (r *MySQLRepository) HasActiveJobsByVersionID(ctx context.Context, versionID uint64) (bool, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM jobs WHERE version_id = ? AND status IN (?, ?)`,
		versionID, entity.JobStatusPending, entity.JobStatusRunning,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *MySQLRepository) DeleteByVersionID(ctx context.Context, versionID uint64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM jobs WHERE version_id = ?`, versionID)
	return err
}

// HasActiveJobsByAssemblyVersionID is the per-assembly counterpart of
// HasActiveJobsByVersionID, used to guard deleting a single Assembly Version.
func (r *MySQLRepository) HasActiveJobsByAssemblyVersionID(ctx context.Context, assemblyVersionID uint64) (bool, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM jobs WHERE assembly_version_id = ? AND status IN (?, ?)`,
		assemblyVersionID, entity.JobStatusPending, entity.JobStatusRunning,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// DeleteByAssemblyVersionID is the per-assembly counterpart of
// DeleteByVersionID, used when deleting a single Assembly Version rather than
// the whole Database Version.
func (r *MySQLRepository) DeleteByAssemblyVersionID(ctx context.Context, assemblyVersionID uint64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM jobs WHERE assembly_version_id = ?`, assemblyVersionID)
	return err
}
