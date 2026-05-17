package gff3

import (
	"errors"
	"strings"
)

type GFF3GeneID struct {
	Current  string
	Previous []string
}

var ErrNilGeneXRefID = errors.New("cannot find gene xref id")

// GeneralGeneIDFinder extracts a gene ID using geneIDKey, which can be:
//
//   - Simple ("ID"): iterates record.Attributes["ID"], strips trimPrefixChars /
//     trimSuffixChars from each value, returns the first non-empty result.
//
//   - Nested ("Dbxref.GeneID"): looks up record.Attributes["Dbxref"]; each
//     attribute value is a comma-separated list of "dbname:dbvalue" pairs.
//     Finds pairs whose dbname equals "GeneID", then applies trimPrefixChars /
//     trimSuffixChars to the dbvalue before returning it.
//
// If oldGeneIDKeys is non-empty, the same extraction is applied to each key
// and the results are appended to GFF3GeneID.Previous.
func GeneralGeneIDFinder(record GFF3Record, geneIDKey string, trimPrefixChars, trimSuffixChars int, oldGeneIDKeys []string) (GFF3GeneID, error) {
	trim := func(s string) string {
		if len(s) <= trimPrefixChars+trimSuffixChars {
			return ""
		}
		return s[trimPrefixChars : len(s)-trimSuffixChars]
	}

	extractIDs := func(key string) []string {
		var ids []string
		if before, after, ok := strings.Cut(key, "."); ok {
			attrKey, dbName := before, after
			for _, val := range record.Attributes[attrKey] {
				for part := range strings.SplitSeq(val, ",") {
					part = strings.TrimSpace(part)
					if before0, after0, ok0 := strings.Cut(part, ":"); ok0 && before0 == dbName {
						if id := trim(strings.TrimSpace(after0)); id != "" {
							ids = append(ids, id)
						}
					}
				}
			}
		} else {
			for _, xref := range record.Attributes[key] {
				if id := trim(xref); id != "" {
					ids = append(ids, id)
				}
			}
		}
		return ids
	}

	ids := extractIDs(geneIDKey)
	if len(ids) == 0 {
		return GFF3GeneID{}, ErrNilGeneXRefID
	}

	result := GFF3GeneID{Current: ids[0]}
	for _, oldKey := range oldGeneIDKeys {
		result.Previous = append(result.Previous, extractIDs(oldKey)...)
	}
	return result, nil
}
