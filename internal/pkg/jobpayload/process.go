package jobpayload

// ProcessPayload is the JSON payload stored in jobs.payload for file
// processing jobs. It carries everything the worker needs to locate and
// process the uploaded file.
type ProcessPayload struct {
	UploadFileID string `json:"upload_file_id"`
	VersionID    uint64 `json:"version_id"`
	FilePath     string `json:"file_path"`
	FileType     string `json:"file_type"`
	// Order and Algorithm are set for orthology.tsv jobs.
	Order     string `json:"order,omitempty"`
	Algorithm string `json:"algorithm,omitempty"`
	// SynonymIndexName and SynonymAliasName are pre-computed at job creation time
	// and shared across all synonym jobs for the same version (GENOMIC.GFF,
	// FB_SYNONYM.TSV, FBGN_FBTR_FBPP.TSV) so they all write to the same index.
	SynonymIndexName string `json:"synonym_index_name,omitempty"`
	SynonymAliasName string `json:"synonym_alias_name,omitempty"`
}
