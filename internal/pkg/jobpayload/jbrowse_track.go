package jobpayload

// JBrowseTrackPayload is the JSON payload for JBROWSE.TRACK jobs.
// FileID is stored so the handler can construct "track-<file.id>" without a DB lookup.
// AssemblyID (entity.AssemblyVersion.AssemblyID) is the opaque JBrowse2 assembly
// key this track attaches to; VersionName is kept alongside for display/logging
// purposes only.
type JBrowseTrackPayload struct {
	VersionName            string `json:"version_name"`
	AssemblyID             string `json:"assembly_id"`
	FilePath               string `json:"file_path"`
	TrackName              string `json:"track_name"`
	FileID                 string `json:"file_id"`
	Category               string `json:"category,omitempty"`
	SelectInDefaultSession bool   `json:"select_in_default_session,omitempty"`
}
