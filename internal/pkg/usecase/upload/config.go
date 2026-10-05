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

	entity.FileTypeOrthologyBundle: {},
}

// concurrentFileTypes may have several jobs pending/running for the same version,
// so uploading one is not rejected while another of its type is in flight.
// Each file is processed independently: jbrowse tracks get their own track ID,
// and orthology documents are keyed by file ID inside the version's shared index.
// Bundles only fan out into those two types.
var concurrentFileTypes = map[string]struct{}{
	entity.FileTypeJBrowseTrack:    {},
	entity.FileTypeOrthologyTSV:    {},
	entity.FileTypeOrthologyBundle: {},
}

// isVersionScoped reports whether a file type belongs to the whole Database Version
// rather than to one Assembly Version. Such uploads take no "assembly" metadata.
func isVersionScoped(fileType string) bool {
	return fileType == entity.FileTypeOrthologyTSV || fileType == entity.FileTypeOrthologyBundle
}

// fileNamePattern blocks path separators and control characters. Path traversal
// (names starting with "..") is checked separately in the upload handler.
var fileNamePattern = regexp.MustCompile(`^[^\x00-\x1f/\\]{1,255}$`)
