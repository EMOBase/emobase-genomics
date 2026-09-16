package jobpayload

// RemoveBlastPayload is the JSON payload for REMOVE_BLAST jobs.
// AssemblyID (entity.AssemblyVersion.AssemblyID) identifies which assembly's
// stale BLAST database to remove. VersionName identifies the release, used
// to promote this version's JBrowse2 assembly to the default view once all
// blast jobs for it are done.
type RemoveBlastPayload struct {
	AssemblyID  string `json:"assembly_id"`
	VersionName string `json:"version_name"`
}
