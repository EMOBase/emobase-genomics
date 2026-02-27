package entity

type Orthology struct {
	Group     string
	Orthologs []string
}

func (o *Orthology) GetID() string {
	return o.Group
}
