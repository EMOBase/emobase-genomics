package entity

type Sequence struct {
	Name     string `json:"name"`
	Species  string `json:"species"`
	Sequence string `json:"sequence"`
	Type     string `json:"type"`
}

func (s *Sequence) GetID() string {
	return s.Species + ":" + s.Type + ":" + s.Name
}
