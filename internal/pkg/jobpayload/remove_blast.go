package jobpayload

// RemoveBlastPayload is the JSON payload for REMOVE_BLAST jobs.
// VersionID/AssemblyVersionID identify the assembly (entity.AssemblyVersion)
// whose stale BLAST database to remove; the handler formats them into the
// opaque per-assembly output path (entity.FormatAssemblyID) at build time.
// VersionName identifies the release, used to promote this version's
// JBrowse2 assembly to the default view once all blast jobs for it are done.
type RemoveBlastPayload struct {
	VersionID         uint64 `json:"version_id"`
	AssemblyVersionID uint64 `json:"assembly_version_id"`
	VersionName       string `json:"version_name"`
}
