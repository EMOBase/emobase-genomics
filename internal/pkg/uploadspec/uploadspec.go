// Package uploadspec holds per-file-type metadata validation, job
// construction and stored file naming shared by single-file uploads
// (usecase/upload) and bundle extraction (worker BundleHandler), so both paths
// accept the same metadata, enqueue identical jobs and name files alike.
package uploadspec

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	"github.com/EMOBase/emobase-genomics/internal/pkg/jobpayload"
)

// ValidateOrthologyMeta validates the metadata required by orthology.tsv files.
func ValidateOrthologyMeta(meta map[string]string) error {
	if strings.TrimSpace(meta["order"]) == "" {
		return errors.New("orthology.tsv uploads require an \"order\" metadata field")
	}
	if _, err := strconv.Atoi(meta["order"]); err != nil {
		return errors.New("orthology.tsv \"order\" metadata field must be an integer")
	}
	if strings.TrimSpace(meta["algorithm"]) == "" {
		return errors.New("orthology.tsv uploads require an \"algorithm\" metadata field")
	}
	return nil
}

// ValidateJBrowseTrackMeta validates the metadata required by jbrowse.track files.
func ValidateJBrowseTrackMeta(meta map[string]string) error {
	if strings.TrimSpace(meta["trackName"]) == "" {
		return errors.New("jbrowse.track uploads require a \"trackName\" metadata field")
	}
	return nil
}

// NewOrthologyTSVJob builds (but does not persist) an ORTHOLOGY.TSV job. Orthology
// files belong to the whole version, so the job has no assembly.
// meta must already have passed ValidateOrthologyMeta.
func NewOrthologyTSVJob(versionID uint64, fileID, filePath string, meta map[string]string) (*entity.Job, error) {
	order, _ := strconv.Atoi(meta["order"])
	return newJob(versionID, nil, fileID, entity.JobTypeOrthologyTSV, jobpayload.OrthologyTSVPayload{
		UploadFileID: fileID,
		VersionID:    versionID,
		FilePath:     filePath,
		Order:        order,
		Algorithm:    strings.TrimSpace(meta["algorithm"]),
	})
}

// NewJBrowseTrackJob builds (but does not persist) a JBROWSE.TRACK job for the
// given assembly. meta must already have passed ValidateJBrowseTrackMeta.
func NewJBrowseTrackJob(versionID uint64, assemblyVersionID *uint64, fileID, filePath string, meta map[string]string) (*entity.Job, error) {
	if assemblyVersionID == nil {
		return nil, errors.New("jbrowse.track files must belong to an assembly")
	}
	selectInDefaultSession, _ := strconv.ParseBool(meta["selectInDefaultSession"])
	return newJob(versionID, assemblyVersionID, fileID, entity.JobTypeJBrowseTrack, jobpayload.JBrowseTrackPayload{
		VersionID:              versionID,
		AssemblyVersionID:      *assemblyVersionID,
		FilePath:               filePath,
		TrackName:              strings.TrimSpace(meta["trackName"]),
		FileID:                 fileID,
		Category:               strings.TrimSpace(meta["category"]),
		SelectInDefaultSession: selectInDefaultSession,
	})
}

func newJob(versionID uint64, assemblyVersionID *uint64, fileID, jobType string, payload any) (*entity.Job, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal %s payload: %w", jobType, err)
	}
	p := json.RawMessage(raw)
	now := time.Now().UTC()
	return &entity.Job{
		VersionID:         versionID,
		AssemblyVersionID: assemblyVersionID,
		FileID:            &fileID,
		Type:              jobType,
		Description:       entity.JobDescriptions[jobType],
		Payload:           &p,
		Status:            entity.JobStatusPending,
		CreatedAt:         now,
		UpdatedAt:         now,
	}, nil
}

// BundleSpec describes how one bundle file type is extracted into child files.
type BundleSpec struct {
	ChildFileType string
	ChildJobType  string
	// Columns is the manifest.csv header; "fileName" names the archive entry.
	Columns []string
	// UniqueColumn, if set, must not repeat across rows of one bundle.
	UniqueColumn string
	Validate     func(meta map[string]string) error
	NewJob       func(versionID uint64, assemblyVersionID *uint64, fileID, filePath string, meta map[string]string) (*entity.Job, error)
}

// ManifestFileName is the archive entry that describes every other entry.
const ManifestFileName = "manifest.csv"

// Bundles maps each bundle file type to its spec.
var Bundles = map[string]BundleSpec{
	entity.FileTypeOrthologyBundle: {
		ChildFileType: entity.FileTypeOrthologyTSV,
		ChildJobType:  entity.JobTypeOrthologyTSV,
		Columns:       []string{"fileName", "order", "algorithm"},
		// No UniqueColumn: order+algorithm form a shared source label
		// ("{order}.{algorithm}"), so several files may legitimately share one.
		Validate: ValidateOrthologyMeta,
		NewJob: func(versionID uint64, _ *uint64, fileID, filePath string, meta map[string]string) (*entity.Job, error) {
			return NewOrthologyTSVJob(versionID, fileID, filePath, meta)
		},
	},
	entity.FileTypeJBrowseTrackBundle: {
		ChildFileType: entity.FileTypeJBrowseTrack,
		ChildJobType:  entity.JobTypeJBrowseTrack,
		Columns:       []string{"fileName", "trackName", "category", "selectInDefaultSession"},
		UniqueColumn:  "trackName",
		Validate:      ValidateJBrowseTrackMeta,
		NewJob:        NewJBrowseTrackJob,
	},
}

// storedTagLength is how many characters of the upload ID tag a stored file
// name. Tags only need to differ between same-named files in one folder, so 8
// hex characters (32 random bits) are plenty.
const storedTagLength = 8

// StoredFileName returns the on-disk name of an uploaded file: name with a
// short tag from the upload's ID inserted before its first dot
// ("genomic.gff.gz" -> "genomic-1a2b3c4d.gff.gz"). Two uploads of the same
// name then never share a path, so neither can overwrite or delete the other's
// file. The tag goes before the extensions, which handlers and scripts use to
// recognize a file's format, and after the start of the name, which picks the
// synonym parser.
func StoredFileName(name, uploadID string) string {
	tag := strings.ReplaceAll(uploadID, "-", "")
	if len(tag) > storedTagLength {
		tag = tag[:storedTagLength]
	}
	// Search from index 1 so a leading dot (".hidden.gz") stays in the stem.
	if len(name) > 1 {
		if i := strings.IndexByte(name[1:], '.'); i >= 0 {
			return name[:i+1] + "-" + tag + name[i+1:]
		}
	}
	return name + "-" + tag
}
