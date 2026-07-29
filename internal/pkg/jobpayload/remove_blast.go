package jobpayload

// RemoveBlastPayload is the JSON payload for REMOVE_BLAST jobs.
// VersionName identifies the release, used to promote this version's
// JBrowse2 assembly to the default view once all blast jobs for it are done.
type RemoveBlastPayload struct {
	VersionName string `json:"version_name"`
}
