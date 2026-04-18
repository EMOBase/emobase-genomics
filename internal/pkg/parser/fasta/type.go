package fasta

// FastaRecord holds a single FASTA entry parsed from a file.
type FastaRecord struct {
	Line     int
	Header   string
	Sequence string
}
