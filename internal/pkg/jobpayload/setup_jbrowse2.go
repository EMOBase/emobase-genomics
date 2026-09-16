package jobpayload

// SetupJBrowse2FNAPayload is the JSON payload for GENOMIC.FNA:SETUP_JBROWSE2 jobs.
// The path points to a gzip-compressed file; the setup script handles decompression.
// AssemblyID (entity.AssemblyVersion.AssemblyID) is the opaque JBrowse2 assembly
// key; VersionName is kept alongside for display/logging purposes only.
type SetupJBrowse2FNAPayload struct {
	VersionName    string `json:"version_name"`
	AssemblyID     string `json:"assembly_id"`
	GenomicFNAPath string `json:"genomic_fna_path"`
}

// SetupJBrowse2GFFPayload is the JSON payload for GENOMIC.GFF:SETUP_JBROWSE2 jobs.
// The path points to a gzip-compressed file; the setup script handles decompression.
// AssemblyID (entity.AssemblyVersion.AssemblyID) is the opaque JBrowse2 assembly
// key. DisplayLabel is the human-readable "{versionName} — {assembly.name}"
// text used for the track title; VersionName is kept alongside for
// logging purposes only.
type SetupJBrowse2GFFPayload struct {
	VersionName     string `json:"version_name"`
	AssemblyID      string `json:"assembly_id"`
	DisplayLabel    string `json:"display_label"`
	GenomicGFFPath  string `json:"genomic_gff_path"`
	GeneIDKey       string `json:"gene_id_key,omitempty"`
	GeneLinkBase    string `json:"gene_link_base,omitempty"`
	TrimPrefixChars int    `json:"trim_prefix_chars,omitempty"`
	TrimSuffixChars int    `json:"trim_suffix_chars,omitempty"`
}
