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
	// SynonymFiles holds the paths of versionless synonym files (fb_synonym,
	// fbgn_fbtr_fbpp) discovered at SYNONYM job creation time.
	SynonymFiles []string `json:"synonym_files,omitempty"`
}
