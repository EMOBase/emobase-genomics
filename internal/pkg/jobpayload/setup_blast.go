package jobpayload

// SetupBlastPayload is the JSON payload for SETUP_BLAST jobs.
// FilePath points to the (gzip-compressed) input file for makeblastdb.
// VersionID/AssemblyVersionID identify the assembly (entity.AssemblyVersion)
// whose BLAST database this is; the handler formats them into the opaque
// per-assembly output path (entity.FormatAssemblyID) at build time.
// AssemblyName is the assembly's human-readable label, used to build the
// SequenceServer-facing -title.
// VersionName identifies the release, used to promote this version's
// JBrowse2 assembly to the default view once all blast jobs for it are done.
type SetupBlastPayload struct {
	FilePath          string `json:"file_path"`
	VersionID         uint64 `json:"version_id"`
	AssemblyVersionID uint64 `json:"assembly_version_id"`
	AssemblyName      string `json:"assembly_name"`
	VersionName       string `json:"version_name"`
}
