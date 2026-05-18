package jobpayload

// SetupJBrowse2FNAPayload is the JSON payload for GENOMIC.FNA:SETUP_JBROWSE2 jobs.
// The path points to a gzip-compressed file; the setup script handles decompression.
type SetupJBrowse2FNAPayload struct {
	VersionName    string `json:"version_name"`
	GenomicFNAPath string `json:"genomic_fna_path"`
}

// SetupJBrowse2GFFPayload is the JSON payload for GENOMIC.GFF:SETUP_JBROWSE2 jobs.
// The path points to a gzip-compressed file; the setup script handles decompression.
type SetupJBrowse2GFFPayload struct {
	VersionName     string `json:"version_name"`
	GenomicGFFPath  string `json:"genomic_gff_path"`
	GeneIDKey       string `json:"gene_id_key,omitempty"`
	GeneLinkBase    string `json:"gene_link_base,omitempty"`
	TrimPrefixChars int    `json:"trim_prefix_chars,omitempty"`
	TrimSuffixChars int    `json:"trim_suffix_chars,omitempty"`
}
