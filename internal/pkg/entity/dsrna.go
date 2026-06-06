package entity

type DsRNA struct {
	Gene        string `json:"gene"`
	Seq         string `json:"seq"`
	LeftPrimer  string `json:"leftPrimer,omitempty"`
	RightPrimer string `json:"rightPrimer,omitempty"`
}

func (d DsRNA) GetID() string { return d.Gene }
