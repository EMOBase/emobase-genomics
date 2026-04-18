package jobpayload

// SetupJBrowse2Payload is the JSON payload for GENOMIC.FNA:SETUP_JBROWSE2 jobs.
// Both paths point to gzip-compressed files; the setup script handles decompression.
type SetupJBrowse2Payload struct {
	VersionName    string `json:"version_name"`
	GenomicFNAPath string `json:"genomic_fna_path"`
	GenomicGFFPath string `json:"genomic_gff_path"`
}
