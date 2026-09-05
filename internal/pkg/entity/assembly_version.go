package entity

import "time"

// AssemblyVersion is a single species' assembly within a Database Version
// (entity.Version). Every Assembly Version is symmetric: there is no
// "primary"/"default" flag on this entity — nothing here is special-cased.
type AssemblyVersion struct {
	ID        uint64    `db:"id"         json:"id"`
	VersionID uint64    `db:"version_id" json:"versionId"`
	Name      string    `db:"name"       json:"name"`
	Species   string    `db:"species"    json:"species"`
	CreatedAt time.Time `db:"created_at" json:"createdAt"`
	CreatedBy string    `db:"created_by" json:"createdBy"`
	UpdatedAt time.Time `db:"updated_at" json:"updatedAt"`
	UpdatedBy string    `db:"updated_by" json:"updatedBy"`
}
