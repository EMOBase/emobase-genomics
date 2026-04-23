package entity

import (
	"encoding/json"
	"time"
)

type JobStatus string

const (
	JobStatusPending JobStatus = "PENDING"
	JobStatusRunning JobStatus = "RUNNING"
	JobStatusDone    JobStatus = "DONE"
	JobStatusFailed  JobStatus = "FAILED"
)

// Job type constants — must match the values stored in jobs.type.
const (
	JobTypeGenomicGFF         = "GENOMIC.GFF"
	JobTypeRNAFNA             = "RNA.FNA"
	JobTypeCDSFNA             = "CDS.FNA"
	JobTypeProteinFAA         = "PROTEIN.FAA"
	JobTypeOrthologyTSV       = "ORTHOLOGY.TSV"
	JobTypeOrthologyTSVDelete = "ORTHOLOGY.TSV:DELETE"

	JobTypeGenomicGFFSynonym = "GENOMIC.GFF:SYNONYM"

	// SETUP_BLAST jobs run makeblastdb to build a SequenceServer-compatible
	// BLAST database from the processed file.
	JobTypeGenomicFNASetupBlast = "GENOMIC.FNA:SETUP_BLAST"
	JobTypeProteinFAASetupBlast = "PROTEIN.FAA:SETUP_BLAST"
	JobTypeRNAFNASetupBlast     = "RNA.FNA:SETUP_BLAST"

	// SETUP_JBROWSE2 jobs build JBrowse2 genome browser tracks.
	// FNA job runs first (add-assembly). GFF job runs after FNA:SETUP_JBROWSE2 is done.
	JobTypeGenomicFNASetupJBrowse2 = "GENOMIC.FNA:SETUP_JBROWSE2"
	JobTypeGenomicGFFSetupJBrowse2 = "GENOMIC.GFF:SETUP_JBROWSE2"
)

// JobDescriptions maps each job type to a human-readable title shown in the UI.
var JobDescriptions = map[string]string{
	JobTypeGenomicGFF:              "Parse genomic GFF file",
	JobTypeRNAFNA:                  "Parse RNA FASTA file",
	JobTypeCDSFNA:                  "Parse CDS FASTA file",
	JobTypeProteinFAA:              "Parse protein FASTA file",
	JobTypeOrthologyTSV:            "Parse orthology TSV file",
	JobTypeOrthologyTSVDelete:      "Delete orthology TSV file",
	JobTypeGenomicGFFSynonym:       "Build gene synonyms",
	JobTypeGenomicFNASetupBlast:    "Setup genome BLAST database",
	JobTypeProteinFAASetupBlast:    "Setup protein BLAST database",
	JobTypeRNAFNASetupBlast:        "Setup RNA BLAST database",
	JobTypeGenomicFNASetupJBrowse2: "Setup JBrowse2 assembly",
	JobTypeGenomicGFFSetupJBrowse2: "Setup JBrowse2 annotation track",
}

type Job struct {
	ID             uint64           `db:"id"`
	VersionID      uint64           `db:"version_id"`
	FileID         *string          `db:"file_id"`
	Type           string           `db:"type"`
	Description    string           `db:"description"`
	Payload        *json.RawMessage `db:"payload"`
	Status         JobStatus        `db:"status"`
	ResultMetadata *json.RawMessage `db:"result_metadata"`
	CreatedAt      time.Time        `db:"created_at"`
	UpdatedAt      time.Time        `db:"updated_at"`
	StartedAt      *time.Time       `db:"started_at"`
	CompletedAt    *time.Time       `db:"completed_at"`
}
