package jobpayload

// GenomicGFFPayload is the JSON payload for GENOMIC.GFF jobs.
// Species is the owning Assembly Version's species code, used as the ES
// gene-ID prefix. Index names are keyed by AssemblyVersionID instead, since a
// species code does not identify an assembly.
// DisplayLabel is carried along so the worker-side deferred
// GENOMIC.GFF:SETUP_JBROWSE2 enqueue (triggered once GENOMIC.FNA:SETUP_JBROWSE2
// completes, in usecase/worker/handlers/setup_jbrowse2.go) can rebuild that
// job's payload without a further DB round trip — the assembly's numeric IDs
// don't need to be carried here too, since that enqueue already has them
// from the completed GENOMIC.FNA:SETUP_JBROWSE2 job it was triggered by.
type GenomicGFFPayload struct {
	UploadFileID      string   `json:"upload_file_id"`
	VersionID         uint64   `json:"version_id"`
	AssemblyVersionID uint64   `json:"assembly_version_id"`
	FilePath          string   `json:"file_path"`
	Species           string   `json:"species"`
	DisplayLabel      string   `json:"display_label"`
	GeneIDKey         string   `json:"gene_id_key"`
	TrimPrefixChars   int      `json:"trim_prefix_chars"`
	TrimSuffixChars   int      `json:"trim_suffix_chars"`
	OldGeneIDKeys     []string `json:"old_gene_id_keys,omitempty"`
}
