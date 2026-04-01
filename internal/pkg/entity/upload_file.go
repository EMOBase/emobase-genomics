package entity

import (
	"encoding/json"
	"time"
)

type UploadStatus string

const (
	UploadStatusUploading UploadStatus = "UPLOADING"
	UploadStatusCompleted UploadStatus = "COMPLETED"
	UploadStatusFailed    UploadStatus = "FAILED"
)

type UploadFile struct {
	ID           string           `db:"id"`
	VersionID    uint64           `db:"version_id"`
	FilePath     string           `db:"file_path"`
	FileType     string           `db:"file_type"`
	Metadata     *json.RawMessage `db:"metadata"`
	UploadStatus UploadStatus     `db:"upload_status"`
	CreatedAt    time.Time        `db:"created_at"`
	CreatedBy    string           `db:"created_by"`
	CompletedAt  *time.Time       `db:"completed_at"`
	DeletedAt    *time.Time       `db:"deleted_at"`
	DeletedBy    *string          `db:"deleted_by"`
}
