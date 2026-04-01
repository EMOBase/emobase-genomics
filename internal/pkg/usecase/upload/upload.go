package upload

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/EMOBase/emobase-genomics/internal/pkg/auth"
	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	"github.com/rs/zerolog/log"
	"github.com/tus/tusd/v2/pkg/filelocker"
	"github.com/tus/tusd/v2/pkg/filestore"
	tusd "github.com/tus/tusd/v2/pkg/handler"
)

// allowedFileTypes is the set of accepted values for the fileType metadata field.
var allowedFileTypes = map[string]struct{}{
	"genomic.fna":   {},
	"genomic.gff":   {},
	"rna.fna":       {},
	"cds.fna":       {},
	"protein.faa":   {},
	"orthology.tsv": {},
}

// fileNamePattern allows filenames up to 255 characters starting with an
// alphanumeric character, followed by letters, digits, dots, dashes, or underscores.
var fileNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,254}$`)

type UseCase struct {
	Handler     *tusd.Handler
	uploadDir   string
	versionRepo IVersionRepository
	jobRepo     IJobRepository
	uploadRepo  IUploadFileRepository
}

func New(
	uploadDir string,
	versionRepo IVersionRepository,
	jobRepo IJobRepository,
	uploadRepo IUploadFileRepository,
) (*UseCase, error) {
	store := filestore.New(uploadDir)
	locker := filelocker.New(uploadDir)

	composer := tusd.NewStoreComposer()
	store.UseIn(composer)
	locker.UseIn(composer)

	uc := &UseCase{
		uploadDir:   uploadDir,
		versionRepo: versionRepo,
		jobRepo:     jobRepo,
		uploadRepo:  uploadRepo,
	}

	handler, err := tusd.NewHandler(tusd.Config{
		BasePath:                "/uploads",
		StoreComposer:           composer,
		DisableDownload:         true,
		NotifyCreatedUploads:    true,
		NotifyCompleteUploads:   true,
		PreUploadCreateCallback: uc.handlePreUploadCreate,
	})
	if err != nil {
		log.Error().Err(err).Msg("unable to create tusd handler")
		return nil, err
	}

	uc.Handler = handler
	go uc.processEvents()

	return uc, nil
}

func (uc *UseCase) handlePreUploadCreate(hook tusd.HookEvent) (tusd.HTTPResponse, tusd.FileInfoChanges, error) {
	meta := hook.Upload.MetaData

	// 1. Validate fileType.
	fileType := meta["fileType"]
	if _, ok := allowedFileTypes[fileType]; !ok {
		allowed := make([]string, 0, len(allowedFileTypes))
		for k := range allowedFileTypes {
			allowed = append(allowed, k)
		}
		return errResponse(http.StatusBadRequest,
			fmt.Sprintf("invalid fileType %q, must be one of: %s", fileType, strings.Join(allowed, ", ")),
		), tusd.FileInfoChanges{}, errors.New("invalid fileType")
	}

	// 2. Validate fileName.
	fileName := meta["fileName"]
	if !fileNamePattern.MatchString(fileName) {
		return errResponse(http.StatusBadRequest,
			"invalid fileName: must be 1–255 characters, start with a letter or digit, and contain only letters, digits, dots, dashes, or underscores",
		), tusd.FileInfoChanges{}, errors.New("invalid fileName")
	}

	// 3. Check version exists.
	version, err := uc.versionRepo.FindByName(hook.Context, meta["version"])
	if err != nil {
		log.Ctx(hook.Context).Err(err).Msg("version lookup failed in pre-upload hook")
		return errResponse(http.StatusInternalServerError, "internal server error"), tusd.FileInfoChanges{}, err
	}
	if version == nil {
		return errResponse(http.StatusBadRequest,
			fmt.Sprintf("version %q not found", meta["version"]),
		), tusd.FileInfoChanges{}, errors.New("version not found")
	}

	// 4. Reject if an active job of the same type already exists for this version.
	hasActive, err := uc.jobRepo.HasActiveJobOfType(hook.Context, version.ID, fileType)
	if err != nil {
		log.Ctx(hook.Context).Err(err).Msg("job lookup failed in pre-upload hook")
		return errResponse(http.StatusInternalServerError, "internal server error"), tusd.FileInfoChanges{}, err
	}
	if hasActive {
		return errResponse(http.StatusConflict,
			fmt.Sprintf("a job for file type %q is already pending or running for this version", fileType),
		), tusd.FileInfoChanges{}, errors.New("active job conflict")
	}

	// Propagate versionID through metadata so the CreatedUploads handler can
	// use it without a second DB roundtrip.
	newMeta := make(tusd.MetaData, len(meta)+1)
	maps.Copy(newMeta, meta)
	newMeta["_versionID"] = strconv.FormatUint(version.ID, 10)

	return tusd.HTTPResponse{}, tusd.FileInfoChanges{MetaData: newMeta}, nil
}

func (uc *UseCase) processEvents() {
	for {
		select {
		case event := <-uc.Handler.CreatedUploads:
			uc.onCreated(event)

		case event := <-uc.Handler.CompleteUploads:
			uc.onCompleted(event)
		}
	}
}

func (uc *UseCase) onCreated(event tusd.HookEvent) {
	upload := event.Upload

	versionID, err := strconv.ParseUint(upload.MetaData["_versionID"], 10, 64)
	if err != nil {
		log.Error().Err(err).Str("uploadID", upload.ID).Msg("missing or invalid _versionID in upload metadata")
		uc.removeUploadFiles(upload.ID)
		return
	}

	creator := auth.UsernameFromContext(event.Context)

	f := &entity.UploadFile{
		ID:           upload.ID,
		VersionID:    versionID,
		FilePath:     filepath.Join(upload.MetaData["version"], upload.MetaData["fileName"]),
		FileType:     upload.MetaData["fileType"],
		UploadStatus: entity.UploadStatusUploading,
		CreatedBy:    creator,
	}

	if err := uc.uploadRepo.Create(context.Background(), f); err != nil {
		log.Error().Err(err).Str("uploadID", upload.ID).Msg("failed to create upload_file record, cleaning up")
		uc.removeUploadFiles(upload.ID)
		return
	}

	log.Info().
		Str("uploadID", upload.ID).
		Str("fileType", f.FileType).
		Str("filePath", f.FilePath).
		Msg("upload created, record saved")
}

func (uc *UseCase) onCompleted(event tusd.HookEvent) {
	upload := event.Upload

	version := upload.MetaData["version"]
	fileName := upload.MetaData["fileName"]

	if version == "" || fileName == "" {
		log.Warn().Str("uploadID", upload.ID).Msg("missing version or fileName in metadata, skipping move")
		return
	}

	dstDir := filepath.Join(uc.uploadDir, version)
	srcPath := filepath.Join(uc.uploadDir, upload.ID)
	dstPath := filepath.Join(dstDir, fileName)

	if err := os.MkdirAll(dstDir, 0755); err != nil {
		log.Error().Err(err).Str("dir", dstDir).Msg("failed to create version directory")
		return
	}

	if err := os.Rename(srcPath, dstPath); err != nil {
		log.Error().Err(err).Str("src", srcPath).Str("dst", dstPath).Msg("failed to move upload")
		return
	}

	infoPath := filepath.Join(uc.uploadDir, upload.ID+".info")
	if err := os.Remove(infoPath); err != nil {
		log.Warn().Err(err).Msg("failed to remove .info file")
	}

	log.Info().
		Str("uploadID", upload.ID).
		Str("path", dstPath).
		Msg("upload complete, file moved")
}

func (uc *UseCase) removeUploadFiles(uploadID string) {
	for _, path := range []string{
		filepath.Join(uc.uploadDir, uploadID),
		filepath.Join(uc.uploadDir, uploadID+".info"),
	} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			log.Error().Err(err).Str("path", path).Msg("failed to remove upload file during cleanup")
		}
	}
}

func errResponse(statusCode int, message string) tusd.HTTPResponse {
	return tusd.HTTPResponse{
		StatusCode: statusCode,
		Body:       fmt.Sprintf(`{"message":%q}`, message),
		Header:     tusd.HTTPHeader{"Content-Type": "application/json"},
	}
}
