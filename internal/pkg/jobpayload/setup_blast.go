package jobpayload

// SetupBlastPayload is the JSON payload for SETUP_BLAST jobs.
// FilePath points to the (gzip-compressed) input file for makeblastdb.
// AssemblyID (entity.AssemblyVersion.AssemblyID) identifies which assembly's
// BLAST database this is, used to build a per-assembly output path.
// VersionName identifies the release, used to promote this version's
// JBrowse2 assembly to the default view once all blast jobs for it are done.
type SetupBlastPayload struct {
	FilePath    string `json:"file_path"`
	AssemblyID  string `json:"assembly_id"`
	VersionName string `json:"version_name"`
}
