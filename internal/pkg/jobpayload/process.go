package jobpayload

// ProcessPayload is the JSON payload stored in jobs.payload for file
// processing jobs. It carries everything the worker needs to locate and
// process the uploaded file.
type ProcessPayload struct {
	UploadFileID string `json:"upload_file_id"`
	VersionID    uint64 `json:"version_id"`
	FilePath     string `json:"file_path"`
	FileType     string `json:"file_type"`
}
