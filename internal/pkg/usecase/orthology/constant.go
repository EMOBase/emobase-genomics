package orthology

import "errors"

const delimiter = "\t"

var ErrInvalidOrthologyFormat = errors.New("invalid orthology format: expected 3 columns separated by tabs")
