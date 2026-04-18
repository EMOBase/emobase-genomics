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

func NCBIFindGeneID(record GFF3Record) (GFF3GeneID, error) {
	xrefAttributes := record.Attributes["Dbxref"]

	for _, xref := range xrefAttributes {
		parts := strings.SplitN(xref, ":", 2)
		if len(parts) != 2 {
			continue
		}

		if parts[0] == "GeneID" {
			return GFF3GeneID{Current: parts[1]}, nil
		}
	}

	return GFF3GeneID{}, ErrNilGeneXRefID
}
