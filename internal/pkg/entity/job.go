package entity

import (
	"encoding/json"
	"time"
)

type JobStatus string

const (
	JobStatusPending JobStatus = "PENDING"
	JobStatusRunning JobStatus = "RUNNING"
	JobStatusDone    JobStatus = "DONE"
	JobStatusFailed  JobStatus = "FAILED"
)

type Job struct {
	ID             uint64           `db:"id"`
	VersionID      uint64           `db:"version_id"`
	FileID         *string          `db:"file_id"`
	Type           string           `db:"type"`
	Description    string           `db:"description"`
	Payload        *json.RawMessage `db:"payload"`
	Status         JobStatus        `db:"status"`
	ResultMetadata *json.RawMessage `db:"result_metadata"`
	CreatedAt      time.Time        `db:"created_at"`
	UpdatedAt      time.Time        `db:"updated_at"`
	StartedAt      *time.Time       `db:"started_at"`
	CompletedAt    *time.Time       `db:"completed_at"`
}
