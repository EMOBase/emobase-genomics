package upload

import "regexp"

const (
	FileTypeGenomicFNA      = "genomic.fna"
	FileTypeGenomicGFF      = "genomic.gff"
	FileTypeRNAFNA          = "rna.fna"
	FileTypeCDSFNA          = "cds.fna"
	FileTypeProteinFAA      = "protein.faa"
	FileTypeOrthologyTSV    = "orthology.tsv"
	FileTypeFBSynonymTSV    = "fb_synonym.tsv"
	FileTypeFBGNFBTRFBPPTSV = "fbgn_fbtr_fbpp.tsv"
)

// allowedFileTypes is the set of accepted values for the fileType metadata field.
var allowedFileTypes = map[string]struct{}{
	FileTypeGenomicFNA:      {},
	FileTypeGenomicGFF:      {},
	FileTypeRNAFNA:          {},
	FileTypeCDSFNA:          {},
	FileTypeProteinFAA:      {},
	FileTypeOrthologyTSV:    {},
	FileTypeFBSynonymTSV:    {},
	FileTypeFBGNFBTRFBPPTSV: {},
}

// versionlessFileTypes are uploaded once and stored globally — they do not
// belong to any version and do not trigger a processing job on upload.
var versionlessFileTypes = map[string]struct{}{
	FileTypeFBSynonymTSV:    {},
	FileTypeFBGNFBTRFBPPTSV: {},
}

// versionlessFileMeta maps each versionless fileType to the required filename
// prefix (for validation) and the canonical name used when storing the file.
var versionlessFileMeta = map[string]struct {
	prefix        string
	canonicalName string
}{
	FileTypeFBSynonymTSV:    {prefix: "fb_synonym_", canonicalName: "fb_synonym.tsv.gz"},
	FileTypeFBGNFBTRFBPPTSV: {prefix: "fbgn_fbtr_fbpp_", canonicalName: "fbgn_fbtr_fbpp.tsv.gz"},
}

// fileNamePattern blocks path separators and control characters. Path traversal
// (names starting with "..") is checked separately in the upload handler.
var fileNamePattern = regexp.MustCompile(`^[^\x00-\x1f/\\]{1,255}$`)
