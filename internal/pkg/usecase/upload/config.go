package upload

import (
	"regexp"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

// allowedFileTypes is the set of accepted values for the fileType metadata field.
var allowedFileTypes = map[string]struct{}{
	entity.FileTypeGenomicFNA:      {},
	entity.FileTypeGenomicGFF:      {},
	entity.FileTypeRNAFNA:          {},
	entity.FileTypeCDSFNA:          {},
	entity.FileTypeProteinFAA:      {},
	entity.FileTypeOrthologyTSV:    {},
	entity.FileTypeFBSynonymTSV:    {},
	entity.FileTypeFBGNFBTRFBPPTSV: {},
}

// versionlessFileTypes are uploaded once and stored globally — they do not
// belong to any version and do not trigger a processing job on upload.
var versionlessFileTypes = map[string]struct{}{
	entity.FileTypeFBSynonymTSV:    {},
	entity.FileTypeFBGNFBTRFBPPTSV: {},
}

// versionlessFileMeta maps each versionless fileType to the required filename
// prefix (for validation) and the canonical name used when storing the file.
var versionlessFileMeta = map[string]struct {
	prefix        string
	canonicalName string
}{
	entity.FileTypeFBSynonymTSV:    {prefix: "fb_synonym_", canonicalName: "fb_synonym.tsv.gz"},
	entity.FileTypeFBGNFBTRFBPPTSV: {prefix: "fbgn_fbtr_fbpp_", canonicalName: "fbgn_fbtr_fbpp.tsv.gz"},
}

// fileNamePattern blocks path separators and control characters. Path traversal
// (names starting with "..") is checked separately in the upload handler.
var fileNamePattern = regexp.MustCompile(`^[^\x00-\x1f/\\]{1,255}$`)
