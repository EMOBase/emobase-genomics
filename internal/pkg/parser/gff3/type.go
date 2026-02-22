package gff3

type Strand int

// TODO: use this, and ask why we need this
const (
	StrandForward Strand = iota
	StrandReverse
	StrandUnknown
)

type GFF3Record struct {
	SeqID      string
	Source     string
	Type       string
	Start      int
	End        int
	Score      string
	Strand     string
	Phase      string
	Attributes map[string][]string
}
