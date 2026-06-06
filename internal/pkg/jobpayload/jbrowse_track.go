package jobpayload

// JBrowseTrackPayload is the JSON payload for JBROWSE.TRACK jobs.
// FileID is stored so the handler can construct "track-<file.id>" without a DB lookup.
type JBrowseTrackPayload struct {
	VersionName string `json:"version_name"`
	FilePath    string `json:"file_path"`
	TrackName   string `json:"track_name"`
	FileID      string `json:"file_id"`
}
