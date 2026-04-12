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
		`INSERT INTO jobs (version_id, type, payload, status, max_retry_count)
		 VALUES (?, ?, ?, ?, ?)`,
		j.VersionID, j.Type, j.Payload, j.Status, j.MaxRetryCount,
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

func (r *MySQLRepository) FindByID(ctx context.Context, id uint64) (*entity.Job, error) {
	j := &entity.Job{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, version_id, type, payload, status, retry_count, max_retry_count,
		        result_metadata, created_at, updated_at, started_at, completed_at
		 FROM jobs WHERE id = ?`,
		id,
	).Scan(
		&j.ID, &j.VersionID, &j.Type, &j.Payload, &j.Status, &j.RetryCount, &j.MaxRetryCount,
		&j.ResultMetadata, &j.CreatedAt, &j.UpdatedAt, &j.StartedAt, &j.CompletedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return j, nil
}

// FindPending returns up to limit jobs in PENDING status, ordered by created_at.
// Intended for workers to claim jobs.
func (r *MySQLRepository) FindPending(ctx context.Context, limit int) ([]entity.Job, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, version_id, type, payload, status, retry_count, max_retry_count,
		        result_metadata, created_at, updated_at, started_at, completed_at
		 FROM jobs WHERE status = 'PENDING' ORDER BY created_at ASC LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []entity.Job
	for rows.Next() {
		var j entity.Job
		if err := rows.Scan(
			&j.ID, &j.VersionID, &j.Type, &j.Payload, &j.Status, &j.RetryCount, &j.MaxRetryCount,
			&j.ResultMetadata, &j.CreatedAt, &j.UpdatedAt, &j.StartedAt, &j.CompletedAt,
		); err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

// ClaimNextPending atomically selects the oldest PENDING job and marks it
// RUNNING in a single transaction using FOR UPDATE SKIP LOCKED, so concurrent
// workers never claim the same job.
func (r *MySQLRepository) ClaimNextPending(ctx context.Context) (*entity.Job, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	j := &entity.Job{}
	err = tx.QueryRowContext(ctx,
		`SELECT id, version_id, type, payload, status, retry_count, max_retry_count,
		        result_metadata, created_at, updated_at, started_at, completed_at
		 FROM jobs WHERE status = 'PENDING' ORDER BY created_at ASC LIMIT 1 FOR UPDATE SKIP LOCKED`,
	).Scan(
		&j.ID, &j.VersionID, &j.Type, &j.Payload, &j.Status, &j.RetryCount, &j.MaxRetryCount,
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
		`UPDATE jobs SET status = 'RUNNING', started_at = ?, updated_at = ? WHERE id = ?`,
		now, now, j.ID,
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
		`UPDATE jobs SET status = 'PENDING', started_at = NULL, updated_at = NOW()
		 WHERE status = 'RUNNING' AND started_at < ?`,
		stuckBefore,
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (r *MySQLRepository) MarkRunning(ctx context.Context, id uint64) error {
	now := time.Now().UTC()
	_, err := r.db.ExecContext(ctx,
		`UPDATE jobs SET status = 'RUNNING', started_at = ? WHERE id = ? AND status = 'PENDING'`,
		now, id,
	)
	return err
}

func (r *MySQLRepository) MarkDone(ctx context.Context, id uint64, resultMetadata []byte) error {
	now := time.Now().UTC()
	_, err := r.db.ExecContext(ctx,
		`UPDATE jobs SET status = 'DONE', completed_at = ?, result_metadata = ? WHERE id = ?`,
		now, resultMetadata, id,
	)
	return err
}

func (r *MySQLRepository) HasActiveJobOfType(ctx context.Context, versionID uint64, jobType string) (bool, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM jobs
		 WHERE version_id = ? AND type = ? AND status IN ('PENDING', 'RUNNING')`,
		versionID, jobType,
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

	// Build the IN clause placeholders.
	placeholders := make([]byte, 0, len(versionIDs)*2-1)
	args := make([]any, len(versionIDs))
	for i, id := range versionIDs {
		if i > 0 {
			placeholders = append(placeholders, ',')
		}
		placeholders = append(placeholders, '?')
		args[i] = id
	}

	query := `SELECT version_id,
	                 SUM(status = 'RUNNING') AS running_count,
	                 SUM(status = 'FAILED')  AS failed_count,
	                 SUM(status = 'DONE')    AS done_count,
	                 COUNT(*)                AS total_count
	          FROM jobs
	          WHERE version_id IN (` + string(placeholders) + `)
	          GROUP BY version_id`

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

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

func (r *MySQLRepository) MarkFailed(ctx context.Context, id uint64, resultMetadata []byte) error {
	now := time.Now().UTC()
	_, err := r.db.ExecContext(ctx,
		`UPDATE jobs
		 SET status = CASE WHEN retry_count + 1 < max_retry_count THEN 'PENDING' ELSE 'FAILED' END,
		     retry_count = retry_count + 1,
		     completed_at = ?,
		     result_metadata = ?
		 WHERE id = ?`,
		now, resultMetadata, id,
	)
	return err
}
