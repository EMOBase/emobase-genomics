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
	JobTypeGenomicFNA      = "GENOMIC.FNA"
	JobTypeGenomicGFF      = "GENOMIC.GFF"
	JobTypeRNAFNA          = "RNA.FNA"
	JobTypeCDSFNA          = "CDS.FNA"
	JobTypeProteinFAA      = "PROTEIN.FAA"
	JobTypeOrthologyTSV    = "ORTHOLOGY.TSV"
	JobTypeFBSynonymTSV    = "FB_SYNONYM.TSV"
	JobTypeFBGNFBTRFBPPTSV = "FBGN_FBTR_FBPP.TSV"
)
