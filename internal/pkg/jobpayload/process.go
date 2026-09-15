package jobpayload

// ProcessPayload is the JSON payload for simple file processing jobs
// (RNA.FNA, CDS.FNA, PROTEIN.FAA, DSRNA.CSV). Species is the owning Assembly
// Version's species code, used both as the ES gene-ID prefix and as the
// concrete sequence/dsrna index name segment.
type ProcessPayload struct {
	UploadFileID string `json:"upload_file_id"`
	VersionID    uint64 `json:"version_id"`
	FilePath     string `json:"file_path"`
	Species      string `json:"species"`
}
