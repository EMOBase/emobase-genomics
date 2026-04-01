package entity

import "time"

type VersionStatus string

const (
	VersionStatusDraft      VersionStatus = "DRAFT"
	VersionStatusProcessing VersionStatus = "PROCESSING"
	VersionStatusReady      VersionStatus = "READY"
)

type Version struct {
	ID          uint64        `db:"id"`
	Name        string        `db:"name"`
	Description *string       `db:"description"`
	Status      VersionStatus `db:"status"`
	CreatedAt   time.Time     `db:"created_at"`
	CreatedBy   string        `db:"created_by"`
	UpdatedAt   time.Time     `db:"updated_at"`
	UpdatedBy   string        `db:"updated_by"`
}
