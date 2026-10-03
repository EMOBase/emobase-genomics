// Package uploadspec holds per-file-type metadata validation and job
// construction shared by single-file uploads (usecase/upload) and bundle
// extraction (worker BundleHandler), so both paths accept the same metadata
// and enqueue identical jobs.
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

// NewOrthologyTSVJob builds (but does not persist) an ORTHOLOGY.TSV job.
// meta must already have passed ValidateOrthologyMeta.
func NewOrthologyTSVJob(versionID uint64, fileID, filePath string, meta map[string]string) (*entity.Job, error) {
	order, _ := strconv.Atoi(meta["order"])
	return newJob(versionID, fileID, entity.JobTypeOrthologyTSV, jobpayload.OrthologyTSVPayload{
		UploadFileID: fileID,
		VersionID:    versionID,
		FilePath:     filePath,
		Order:        order,
		Algorithm:    strings.TrimSpace(meta["algorithm"]),
	})
}

// NewJBrowseTrackJob builds (but does not persist) a JBROWSE.TRACK job.
// meta must already have passed ValidateJBrowseTrackMeta.
func NewJBrowseTrackJob(versionID uint64, versionName, fileID, filePath string, meta map[string]string) (*entity.Job, error) {
	selectInDefaultSession, _ := strconv.ParseBool(meta["selectInDefaultSession"])
	return newJob(versionID, fileID, entity.JobTypeJBrowseTrack, jobpayload.JBrowseTrackPayload{
		VersionName:            versionName,
		FilePath:               filePath,
		TrackName:              strings.TrimSpace(meta["trackName"]),
		FileID:                 fileID,
		Category:               strings.TrimSpace(meta["category"]),
		SelectInDefaultSession: selectInDefaultSession,
	})
}

func newJob(versionID uint64, fileID, jobType string, payload any) (*entity.Job, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal %s payload: %w", jobType, err)
	}
	p := json.RawMessage(raw)
	now := time.Now().UTC()
	return &entity.Job{
		VersionID:   versionID,
		FileID:      &fileID,
		Type:        jobType,
		Description: entity.JobDescriptions[jobType],
		Payload:     &p,
		Status:      entity.JobStatusPending,
		CreatedAt:   now,
		UpdatedAt:   now,
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
	NewJob       func(versionID uint64, versionName, fileID, filePath string, meta map[string]string) (*entity.Job, error)
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
		NewJob: func(versionID uint64, _ string, fileID, filePath string, meta map[string]string) (*entity.Job, error) {
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
