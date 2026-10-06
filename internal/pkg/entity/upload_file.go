package entity

import (
	"encoding/json"
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
