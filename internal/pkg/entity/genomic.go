package entity

type GenomicLocation struct {
	Gene         string `json:"gene"`
	ReferenceSeq string `json:"referenceSeq"`
	Start        int    `json:"start"`
	End          int    `json:"end"`
	Strand       string `json:"strand"`
}

func (g *GenomicLocation) GetID() string {
	return g.Gene
}
