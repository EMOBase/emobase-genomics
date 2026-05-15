package gff3

import (
	"errors"
)

type GFF3GeneID struct {
	Current  string
	Previous []string
}

var ErrNilGeneXRefID = errors.New("cannot find gene xref id")

// GeneralGeneIDFinder extracts a gene ID from the given attribute key,
// stripping a fixed number of leading and trailing characters from each value.
// If oldGeneIDKeys is non-empty, values found under those keys are trimmed the
// same way and appended to GFF3GeneID.Previous.
func GeneralGeneIDFinder(record GFF3Record, geneIDKey string, trimPrefixChars, trimSuffixChars int, oldGeneIDKeys []string) (GFF3GeneID, error) {
	trimValue := func(v string) string {
		if len(v) <= trimPrefixChars+trimSuffixChars {
			return ""
		}
		return v[trimPrefixChars : len(v)-trimSuffixChars]
	}

	var current string
	for _, xref := range record.Attributes[geneIDKey] {
		if id := trimValue(xref); id != "" {
			current = id
			break
		}
	}
	if current == "" {
		return GFF3GeneID{}, ErrNilGeneXRefID
	}

	result := GFF3GeneID{Current: current}
	for _, oldKey := range oldGeneIDKeys {
		for _, xref := range record.Attributes[oldKey] {
			if id := trimValue(xref); id != "" {
				result.Previous = append(result.Previous, id)
			}
		}
	}
	return result, nil
}
