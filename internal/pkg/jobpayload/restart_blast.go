package jobpayload

// RestartBlastPayload is the JSON payload for BLAST:RESTART jobs.
// RemovedAssemblyID is the AssemblyID of the deleted assembly whose BLAST
// databases were removed, kept only to show why the restart ran.
type RestartBlastPayload struct {
	RemovedAssemblyID string `json:"removed_assembly_id"`
}
