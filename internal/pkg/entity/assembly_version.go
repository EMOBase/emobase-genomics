package entity

import (
	"fmt"
	"time"
)

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

// AssemblyID returns an opaque, DB-ID-derived identifier for this assembly,
// used anywhere JBrowse2/BLAST need a stable key that's safe in shell/jq/
// filename contexts and immutable even if Name/Species are later edited —
// unlike a composite built from those free-text fields, which isn't
// guaranteed unique across different Database Versions and can't safely act
// as a shell/jq delimiter.
func (a AssemblyVersion) AssemblyID() string {
	return FormatAssemblyID(a.VersionID, a.ID)
}

// FormatAssemblyID builds the same opaque identifier as AssemblyID from a
// Database Version's numeric ID and an Assembly Version's own numeric ID.
// Job payloads that need this identifier carry the two numeric IDs instead
// of the pre-formatted string — easier to read when inspecting a job's
// payload directly (e.g. via SQL) — and call this once, right before
// invoking the JBrowse2/BLAST script that actually needs the opaque form.
func FormatAssemblyID(versionID, assemblyVersionID uint64) string {
	return fmt.Sprintf("v%da%d", versionID, assemblyVersionID)
}
