package jobpayload

// JBrowseTrackPayload is the JSON payload for JBROWSE.TRACK jobs.
// FileID is stored so the handler can construct "track-<file.id>" without a DB lookup.
// VersionID/AssemblyVersionID identify the assembly this track attaches to
// (entity.AssemblyVersion); the handler formats them into the opaque
// JBrowse2 assembly key (entity.FormatAssemblyID) right before invoking the
// track script.
type JBrowseTrackPayload struct {
	VersionID              uint64 `json:"version_id"`
	AssemblyVersionID      uint64 `json:"assembly_version_id"`
	FilePath               string `json:"file_path"`
	TrackName              string `json:"track_name"`
	FileID                 string `json:"file_id"`
	Category               string `json:"category,omitempty"`
	SelectInDefaultSession bool   `json:"select_in_default_session,omitempty"`
}
