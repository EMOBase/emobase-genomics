package worker

import (
	"context"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

// Handler processes a single job. Returning an error marks the job as failed
// (subject to retry). A nil return marks it as done.
type Handler interface {
	Handle(ctx context.Context, job entity.Job) error
}

// Job type constants — must match the values stored in jobs.type.
const (
	JobTypeGenomicGFF   = "GENOMIC.GFF"
	JobTypeRNAFNA       = "RNA.FNA"
	JobTypeCDSFNA       = "CDS.FNA"
	JobTypeProteinFAA   = "PROTEIN.FAA"
	JobTypeOrthologyTSV = "ORTHOLOGY.TSV"
	JobTypeSynonym      = "GENOMIC.GFF:SYNONYM"

	// SETUP_BLAST jobs run makeblastdb to build a SequenceServer-compatible
	// BLAST database from the processed file.
	JobTypeGenomicFNASetupBlast = "GENOMIC.FNA:SETUP_BLAST"
	JobTypeProteinFAASetupBlast = "PROTEIN.FAA:SETUP_BLAST"
	JobTypeRNAFNASetupBlast     = "RNA.FNA:SETUP_BLAST"
)
