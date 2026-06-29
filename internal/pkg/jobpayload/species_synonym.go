package jobpayload

// SpeciesSynonymPayload is the JSON payload for SPECIES.SYNONYM jobs.
// GeneIDKey, TrimPrefixChars, TrimSuffixChars, and OldGeneIDKeys are only
// set when the source file is a GFF3 annotation file.
type SpeciesSynonymPayload struct {
	UploadFileID    string   `json:"upload_file_id"`
	VersionID       uint64   `json:"version_id"`
	FilePath        string   `json:"file_path"`
	Species         string   `json:"species"`
	GeneIDKey       string   `json:"gene_id_key,omitempty"`
	TrimPrefixChars int      `json:"trim_prefix_chars,omitempty"`
	TrimSuffixChars int      `json:"trim_suffix_chars,omitempty"`
	OldGeneIDKeys   []string `json:"old_gene_id_keys,omitempty"`
}
