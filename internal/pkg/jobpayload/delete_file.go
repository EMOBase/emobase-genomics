package jobpayload

type DeleteFilePayload struct {
	UploadFileID string `json:"upload_file_id"`
	DeletedBy    string `json:"deleted_by"`
}
