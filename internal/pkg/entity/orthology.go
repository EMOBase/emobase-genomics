package entity

type Orthology struct {
	Group     string   `json:"group"`
	Orthologs []string `json:"orthologs"`
	FileID    string   `json:"file_id"`
}

func (o *Orthology) GetID() string {
	return o.FileID + ":" + o.Group
}
