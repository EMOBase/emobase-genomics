package gff3

import (
	"errors"
	"strconv"
	"strings"

	"github.com/samber/lo"
)

var (
	ErrInvalidGFF3Line   = errors.New("invalid GFF3 line")
	ErrSeqIDMissing      = errors.New("missing SeqID field")
	ErrTypeMissing       = errors.New("missing Type field")
	ErrStartMissing      = errors.New("missing Start field")
	ErrEndMissing        = errors.New("missing End field")
	ErrInvalidStrand     = errors.New("invalid strand symbol")
	ErrInvalidAttributes = errors.New("invalid attributes field")
)

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

func IsHeaderLine(line string) bool {
	return strings.HasPrefix(line, "#")
}

func IsEmptyLine(line string) bool {
	return line == ""
}

func ParseLine(line string) (GFF3Record, error) {
	fields := strings.Split(line, delimiter)
	if len(fields) != 9 {
		return GFF3Record{}, ErrInvalidGFF3Line
	}

	if fields[0] == "" {
		return GFF3Record{}, ErrSeqIDMissing
	}

	if fields[2] == "" {
		return GFF3Record{}, ErrTypeMissing
	}

	if fields[3] == "" {
		return GFF3Record{}, ErrStartMissing
	}

	if fields[4] == "" {
		return GFF3Record{}, ErrEndMissing
	}

	if _, ok := symbolToStrand[fields[6]]; !ok {
		return GFF3Record{}, ErrInvalidStrand
	}

	start, err := strconv.Atoi(fields[3])
	if err != nil {
		return GFF3Record{}, err
	}

	end, err := strconv.Atoi(fields[4])
	if err != nil {
		return GFF3Record{}, err
	}

	attributes := parseAttributes(fields[8])

	return GFF3Record{
		SeqID:      fields[0],
		Source:     fields[1],
		Type:       fields[2],
		Start:      start,
		End:        end,
		Score:      fields[5],
		Strand:     fields[6],
		Phase:      fields[7],
		Attributes: attributes,
	}, nil
}

func parseAttributes(attrStr string) map[string][]string {
	attrs := strings.Split(attrStr, attributeDelimiter)

	res := make(map[string][]string)
	for _, kvStr := range attrs {
		kvStr = strings.TrimSpace(kvStr)

		parts := strings.SplitN(kvStr, keyValueSeparator, 2)
		if len(parts) != 2 || isEmptyValue(parts[1]) {
			continue
		}

		values := lo.Filter(strings.Split(parts[1], valueDelimiter), func(s string, _ int) bool {
			return !isEmptyValue(s)
		})

		res[parts[0]] = values
	}

	return res
}

func isEmptyValue(s string) bool {
	return s == ""
}
