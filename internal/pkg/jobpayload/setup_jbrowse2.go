package jobpayload

// SetupJBrowse2FNAPayload is the JSON payload for GENOMIC.FNA:SETUP_JBROWSE2 jobs.
// The path points to a gzip-compressed file; the setup script handles decompression.
// VersionID/AssemblyVersionID identify the assembly (entity.AssemblyVersion);
// the handler formats them into the opaque JBrowse2 assembly key
// (entity.FormatAssemblyID) right before invoking the setup script — stored
// as two plain ints rather than the pre-formatted string so the payload
// stays readable when inspected directly (e.g. via SQL).
type SetupJBrowse2FNAPayload struct {
	VersionID         uint64 `json:"version_id"`
	AssemblyVersionID uint64 `json:"assembly_version_id"`
	GenomicFNAPath    string `json:"genomic_fna_path"`
}

// SetupJBrowse2GFFPayload is the JSON payload for GENOMIC.GFF:SETUP_JBROWSE2 jobs.
// The path points to a gzip-compressed file; the setup script handles decompression.
// VersionID/AssemblyVersionID identify the assembly (entity.AssemblyVersion);
// the handler formats them into the opaque JBrowse2 assembly key
// (entity.FormatAssemblyID) right before invoking the setup script.
// DisplayLabel is the human-readable "{versionName} — {assembly.name}" text
// used for the track title (not derivable from the numeric IDs above).
type SetupJBrowse2GFFPayload struct {
	VersionID         uint64 `json:"version_id"`
	AssemblyVersionID uint64 `json:"assembly_version_id"`
	DisplayLabel      string `json:"display_label"`
	GenomicGFFPath    string `json:"genomic_gff_path"`
	GeneIDKey         string `json:"gene_id_key,omitempty"`
	GeneLinkBase      string `json:"gene_link_base,omitempty"`
	TrimPrefixChars   int    `json:"trim_prefix_chars,omitempty"`
	TrimSuffixChars   int    `json:"trim_suffix_chars,omitempty"`
}
