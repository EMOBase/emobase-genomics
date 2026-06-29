package upload

import (
	"regexp"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

// allowedFileTypes is the set of accepted values for the fileType metadata field.
var allowedFileTypes = map[string]struct{}{
	entity.FileTypeGenomicFNA:     {},
	entity.FileTypeGenomicGFF:     {},
	entity.FileTypeRNAFNA:         {},
	entity.FileTypeCDSFNA:         {},
	entity.FileTypeProteinFAA:     {},
	entity.FileTypeOrthologyTSV:   {},
	entity.FileTypeSpeciesSynonym: {},
	entity.FileTypeDsRNACSV:       {},
	entity.FileTypeJBrowseTrack:   {},
}

// fileNamePattern blocks path separators and control characters. Path traversal
// (names starting with "..") is checked separately in the upload handler.
var fileNamePattern = regexp.MustCompile(`^[^\x00-\x1f/\\]{1,255}$`)
