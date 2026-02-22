package sequence

import "errors"

const (
	SEQUENCE_TYPE_CDS        = "CDS"
	SEQUENCE_TYPE_TRANSCRIPT = "TRANSCRIPT"
	SEQUENCE_TYPE_PROTEIN    = "PROTEIN"

	SEQUENCE_TYPE_UNKNOWN = "UNKNOWN"
)

var (
	CDS_SUFFICES        = []string{"cds", "cds_from_genomic"}
	TRANSCRIPT_SUFFICES = []string{"rna", "mrna", "rnas", "mrnas"}
	PROTEIN_SUFFICES    = []string{"protein", "proteins"}

	GZIP_EXTENSIONS  = []string{".gz", ".gzip"}
	FASTA_EXTENSIONS = []string{".fa", ".fasta", ".fna", ".faa"}
)

var (
	ErrInvalidSequenceType = errors.New("invalid sequence type")
)
