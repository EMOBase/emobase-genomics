package job

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

func (r *MySQLRepository) Create(ctx context.Context, j *entity.Job) error {
	result, err := r.db.ExecContext(ctx,
		`INSERT INTO jobs (version_id, file_id, type, description, payload, status)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		j.VersionID, j.FileID, j.Type, j.Description, j.Payload, j.Status,
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

// FindByVersionName returns all jobs for the version with the given name, ordered by creation time.
func (r *MySQLRepository) FindByVersionName(ctx context.Context, versionName string) ([]entity.Job, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT j.id, j.version_id, j.type, j.description, j.status, j.payload, j.result_metadata
		 FROM jobs j
		 JOIN versions v ON v.id = j.version_id
		 WHERE v.name = ?
		 ORDER BY j.created_at ASC`,
		versionName,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var jobs []entity.Job
	for rows.Next() {
		var j entity.Job
		if err := rows.Scan(&j.ID, &j.VersionID, &j.Type, &j.Description, &j.Status, &j.Payload, &j.ResultMetadata); err != nil {
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
		`SELECT id, version_id, file_id, type, description, payload, status,
		        result_metadata, created_at, updated_at, started_at, completed_at
		 FROM jobs WHERE status = ?
		 ORDER BY created_at ASC LIMIT 1 FOR UPDATE SKIP LOCKED`,
		entity.JobStatusPending,
	).Scan(
		&j.ID, &j.VersionID, &j.FileID, &j.Type, &j.Description, &j.Payload, &j.Status,
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

func (r *MySQLRepository) MarkRunning(ctx context.Context, id uint64) error {
	now := time.Now().UTC()
	_, err := r.db.ExecContext(ctx,
		`UPDATE jobs SET status = ?, started_at = ? WHERE id = ? AND status = ?`,
		entity.JobStatusRunning, now, id, entity.JobStatusPending,
	)
	return err
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

// StatusCountsByVersionIDs returns job status counts for each of the given
// version IDs in a single query. Versions with no jobs are not present in the
// returned map.
func (r *MySQLRepository) StatusCountsByVersionIDs(ctx context.Context, versionIDs []uint64) (map[uint64]entity.JobStatusCounts, error) {
	if len(versionIDs) == 0 {
		return map[uint64]entity.JobStatusCounts{}, nil
	}

	placeholders := make([]byte, 0, len(versionIDs)*2-1)
	args := make([]any, 0, len(versionIDs)+4)
	args = append(args, entity.JobStatusRunning, entity.JobStatusFailed, entity.JobStatusDone)
	for i, id := range versionIDs {
		if i > 0 {
			placeholders = append(placeholders, ',')
		}
		placeholders = append(placeholders, '?')
		args = append(args, id)
	}

	query := `SELECT version_id,
	            SUM(status = ?) AS running_count,
	            SUM(status = ?) AS failed_count,
	            SUM(status = ?) AS done_count,
	            COUNT(*)        AS total_count
	        FROM jobs
	        WHERE version_id IN (` + string(placeholders) + `)
	        GROUP BY version_id`

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	result := make(map[uint64]entity.JobStatusCounts, len(versionIDs))
	for rows.Next() {
		var versionID uint64
		var counts entity.JobStatusCounts
		if err := rows.Scan(&versionID, &counts.RunningCount, &counts.FailedCount, &counts.DoneCount, &counts.TotalCount); err != nil {
			return nil, err
		}
		result[versionID] = counts
	}
	return result, rows.Err()
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

// HasNonFailedJobOfType returns true if a PENDING, RUNNING, or DONE job of the
// given type exists for the version. Used to prevent duplicate enqueuing.
func (r *MySQLRepository) HasNonFailedJobOfType(ctx context.Context, versionID uint64, jobType string) (bool, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM jobs
		 WHERE version_id = ? AND type = ? AND status IN (?, ?, ?)`,
		versionID, jobType, entity.JobStatusPending, entity.JobStatusRunning, entity.JobStatusDone,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
