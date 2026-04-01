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
	xrefAttributes, _ := record.Attributes["Dbxref"]

	for _, xref := range xrefAttributes {
		parts := strings.SplitN(xref, ":", 2)
		if len(parts) != 2 {
			continue
		}

		db := parts[0]
		xrefID := parts[1]

		if db == "GeneID" {
			return GFF3GeneID{Current: xrefID}, nil
		}
	}

	return GFF3GeneID{}, ErrNilGeneXRefID
}
