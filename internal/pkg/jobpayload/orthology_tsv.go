package jobpayload

// OrthologyTSVPayload is the JSON payload for ORTHOLOGY.TSV jobs.
type OrthologyTSVPayload struct {
	UploadFileID string `json:"upload_file_id"`
	VersionID    uint64 `json:"version_id"`
	FilePath     string `json:"file_path"`
	Order        int    `json:"order"`
	Algorithm    string `json:"algorithm"`
}
