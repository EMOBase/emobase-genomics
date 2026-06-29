package jobpayload

// SpeciesSynonymPayload is the JSON payload for SPECIES_SYNONYM jobs.
type SpeciesSynonymPayload struct {
	UploadFileID string `json:"upload_file_id"`
	VersionID    uint64 `json:"version_id"`
	FilePath     string `json:"file_path"`
	Species      string `json:"species"`
}
