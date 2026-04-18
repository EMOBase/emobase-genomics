package gff3

type GFF3Record struct {
	Line       int
	SeqID      string
	Source     string
	Type       string
	Start      int
	End        int
	Score      string
	Strand     string
	Phase      string
	Attributes map[string][]string
}

func (r *GFF3Record) GetAttributeFirstValue(key string) (string, bool) {
	values, ok := r.Attributes[key]
	if !ok || len(values) == 0 {
		return "", false
	}

	return values[0], true
}

func (r *GFF3Record) GetID() string {
	res, _ := r.GetAttributeFirstValue("ID")
	return res
}

func (r *GFF3Record) GetParentID() string {
	res, _ := r.GetAttributeFirstValue("Parent")
	return res
}
