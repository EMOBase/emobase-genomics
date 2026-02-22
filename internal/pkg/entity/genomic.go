package entity

type GenomicLocation struct {
	Gene         string
	ReferenceSeq string
	Start        int
	End          int
	Strand       string
}

func (g *GenomicLocation) GetID() string {
	return g.Gene
}
