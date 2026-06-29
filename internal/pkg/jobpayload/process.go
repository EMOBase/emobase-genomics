package jobpayload

// ProcessPayload is the JSON payload for simple file processing jobs
// (RNA.FNA, CDS.FNA, PROTEIN.FAA, DSRNA.CSV).
type ProcessPayload struct {
	UploadFileID string `json:"upload_file_id"`
	VersionID    uint64 `json:"version_id"`
	FilePath     string `json:"file_path"`
}
