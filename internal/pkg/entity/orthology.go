package entity

type Orthology struct {
	Group     string   `json:"group"`
	Orthologs []string `json:"orthologs"`
}

func (o *Orthology) GetID() string {
	return o.Group
}
