package jobpayload

// ProcessPayload is the JSON payload stored in jobs.payload for file
// processing jobs. It carries everything the worker needs to locate and
// process the uploaded file.
type ProcessPayload struct {
	UploadFileID string `json:"upload_file_id"`
	VersionID    uint64 `json:"version_id"`
	FilePath     string `json:"file_path"`
	// Order and Algorithm are set for orthology.tsv jobs.
	Order     int    `json:"order,omitempty"`
	Algorithm string `json:"algorithm,omitempty"`
	// GeneIDKey, TrimPrefixChars, TrimSuffixChars, OldGeneIDKeys are set for genomic.gff jobs.
	GeneIDKey       string   `json:"gene_id_key,omitempty"`
	TrimPrefixChars int      `json:"trim_prefix_chars,omitempty"`
	TrimSuffixChars int      `json:"trim_suffix_chars,omitempty"`
	OldGeneIDKeys   []string `json:"old_gene_id_keys,omitempty"`
}
