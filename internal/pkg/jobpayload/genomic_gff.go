package jobpayload

// GenomicGFFPayload is the JSON payload for GENOMIC.GFF jobs.
type GenomicGFFPayload struct {
	UploadFileID    string   `json:"upload_file_id"`
	VersionID       uint64   `json:"version_id"`
	FilePath        string   `json:"file_path"`
	GeneIDKey       string   `json:"gene_id_key"`
	TrimPrefixChars int      `json:"trim_prefix_chars"`
	TrimSuffixChars int      `json:"trim_suffix_chars"`
	OldGeneIDKeys   []string `json:"old_gene_id_keys,omitempty"`
}
