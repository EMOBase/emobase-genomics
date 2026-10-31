package entity

type Sequence struct {
	Name     string `json:"name"`
	Species  string `json:"species"`
	Sequence string `json:"sequence"`
	Type     string `json:"type"`
	// FileID is the upload the sequence was indexed from, so a replaced
	// file's sequences can be told apart and removed.
	FileID string `json:"file_id"`
}

func (s *Sequence) GetID() string {
	return s.Species + ":" + s.Type + ":" + s.Name
}
