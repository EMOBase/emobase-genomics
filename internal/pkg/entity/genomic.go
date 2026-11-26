package entity

type GenomicLocation struct {
	Gene         string `json:"gene"`
	ReferenceSeq string `json:"referenceSeq"`
	Start        int    `json:"start"`
	End          int    `json:"end"`
	Strand       string `json:"strand"`
	// Index is the concrete ES index a search read this document from, which
	// names its assembly. Set on reads only, never stored.
	Index string `json:"-"`
}

func (g *GenomicLocation) GetID() string {
	return g.Gene
}
