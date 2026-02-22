package gff3

import "errors"

const (
	delimiter = "\t"

	attributeDelimiter = ";"
	valueDelimiter     = ","
	keyValueSeparator  = "="
)

var symbolToStrand = map[string]Strand{
	"+": StrandForward,
	"-": StrandReverse,
	"?": StrandUnknown,
}

var (
	ErrInvalidGFF3Line   = errors.New("invalid GFF3 line")
	ErrSeqIDMissing      = errors.New("missing SeqID field")
	ErrTypeMissing       = errors.New("missing Type field")
	ErrStartMissing      = errors.New("missing Start field")
	ErrEndMissing        = errors.New("missing End field")
	ErrInvalidStrand     = errors.New("invalid strand symbol")
	ErrInvalidAttributes = errors.New("invalid attributes field")
)
