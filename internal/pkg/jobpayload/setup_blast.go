package jobpayload

// SetupBlastPayload is the JSON payload for SETUP_BLAST jobs.
// FilePath points to the (gzip-compressed) input file for makeblastdb.
type SetupBlastPayload struct {
	FilePath string `json:"file_path"`
}
