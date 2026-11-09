package entity

import (
	"encoding/json"
	"slices"
	"time"
)

const (
	FileTypeGenomicFNA     = "genomic.fna"
	FileTypeGenomicGFF     = "genomic.gff"
	FileTypeRNAFNA         = "rna.fna"
	FileTypeCDSFNA         = "cds.fna"
	FileTypeProteinFAA     = "protein.faa"
	FileTypeOrthologyTSV   = "orthology.tsv"
	FileTypeSpeciesSynonym = "species.synonym"
	FileTypeDsRNACSV       = "dsrna.csv"
	FileTypeJBrowseTrack   = "jbrowse.track"

	// Bundle file types: a .tar.gz or .zip archive holding many files of one child type
	// plus a manifest.csv with each file's metadata. A worker job extracts it into
	// independent child upload files; the bundle row itself is kept for audit only.
	FileTypeOrthologyBundle    = "orthology.bundle"
	FileTypeJBrowseTrackBundle = "jbrowse.track.bundle"
)

// BundleFileTypes lists every bundle file type. Bundle rows are excluded from
// version-level file queries since their extracted children are counted instead.
var BundleFileTypes = []string{FileTypeOrthologyBundle, FileTypeJBrowseTrackBundle}

// SingleFileTypes keep one file per assembly: a completed upload replaces the
// assembly's earlier files of its type. Their stored files are removed at
// upload, and their indexed documents once the new file's jobs are done.
var SingleFileTypes = []string{
	FileTypeGenomicFNA, FileTypeGenomicGFF, FileTypeRNAFNA, FileTypeCDSFNA, FileTypeProteinFAA, FileTypeDsRNACSV,
}

// LatestFileTypes are the assembly file types of which only the newest file
// counts toward the assembly's status and release: a single-file type's newer
// upload replaces the older, and a newer bundle supersedes a failed one (an
// extracted bundle's files count under their own type). Every file of any
// other type counts (jbrowse.track, species.synonym), since an assembly keeps
// them all and one failing must not be hidden by a newer one.
var LatestFileTypes = slices.Concat(SingleFileTypes, BundleFileTypes)

// IsSingleFileType reports whether fileType is one of SingleFileTypes.
func IsSingleFileType(fileType string) bool { return slices.Contains(SingleFileTypes, fileType) }

type UploadStatus string

const (
	UploadStatusUploading UploadStatus = "UPLOADING"
	UploadStatusCompleted UploadStatus = "COMPLETED"
	UploadStatusFailed    UploadStatus = "FAILED"
)

type UploadFile struct {
	ID string `db:"id"`
	// VersionID scopes the upload to a Database Version and is always set.
	VersionID uint64 `db:"version_id"`
	// AssemblyVersionID scopes the upload to one species within that Database
	// Version. It is nil for file types shared across every species in the
	// version rather than owned by one assembly (currently only
	// orthology.tsv).
	AssemblyVersionID *uint64          `db:"assembly_version_id"`
	FilePath          string           `db:"file_path"`
	FileType          string           `db:"file_type"`
	FileSize          int64            `db:"file_size"`
	Metadata          *json.RawMessage `db:"metadata"`
	UploadStatus      UploadStatus     `db:"upload_status"`
	CreatedAt         time.Time        `db:"created_at"`
	CreatedBy         string           `db:"created_by"`
	CompletedAt       *time.Time       `db:"completed_at"`
	DeletedAt         *time.Time       `db:"deleted_at"`
	DeletedBy         *string          `db:"deleted_by"`
}
