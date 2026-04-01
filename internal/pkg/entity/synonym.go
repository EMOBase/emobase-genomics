package entity

import (
	"slices"
	"strings"
)

type Synonym struct {
	Gene    string `json:"gene"`
	Type    string `json:"type"`
	Synonym string `json:"synonym"`
}

const (
	SYNONYM_TYPE_CURRENT_ID string = "CURRENT_ID"
	SYNONYM_TYPE_NAME       string = "NAME"
	SYNONYM_TYPE_SYMBOL     string = "SYMBOL"
	SYNONYM_TYPE_DSRNA      string = "DSRNA"
	SYNONYM_TYPE_OLD_ID     string = "OLD_ID"
	SYNONYM_TYPE_TRANSCRIPT string = "TRANSCRIPT"
	SYNONYM_TYPE_PROTEIN    string = "PROTEIN"
	SYNONYM_TYPE_OTHER      string = "OTHER"
)

func (s *Synonym) GetID() string {
	if slices.Contains([]string{"CURRENT_ID", "NAME", "SYMBOL"}, s.Type) {
		return s.Gene + ":" + strings.ToLower(s.Type)
	}

	return s.Gene + ":" + strings.ToLower(s.Type) + ":" + s.Synonym
}
