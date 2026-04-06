package upload

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/EMOBase/emobase-genomics/internal/pkg/auth"
	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	"github.com/EMOBase/emobase-genomics/internal/pkg/jobpayload"
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
	// Handler is the HTTP handler to mount in the router. It wraps tusHandler
	// and may inject an artificial chunk delay in development.
	Handler       http.Handler
	tusHandler    *tusd.Handler
	uploadDir     string
	maxRetryCount int
	versionRepo   IVersionRepository
	jobRepo       IJobRepository
	uploadRepo    IUploadFileRepository
}

func New(
	uploadDir string,
	maxRetryCount int,
	chunkDelay time.Duration,
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
		uploadDir:     uploadDir,
		maxRetryCount: maxRetryCount,
		versionRepo:   versionRepo,
		jobRepo:       jobRepo,
		uploadRepo:    uploadRepo,
	}

	handler, err := tusd.NewHandler(tusd.Config{
		BasePath:                  "/uploads",
		StoreComposer:             composer,
		DisableDownload:           true,
		NotifyCreatedUploads:      true,
		PreUploadCreateCallback:   uc.handlePreUploadCreate,
		PreFinishResponseCallback: uc.handlePreFinish,
	})
	if err != nil {
		log.Error().Err(err).Msg("unable to create tusd handler")
		return nil, err
	}

	uc.tusHandler = handler
	if chunkDelay > 0 {
		log.Warn().Dur("chunk_delay", chunkDelay).Msg("[dev] upload chunk delay enabled")
		uc.Handler = &chunkDelayMiddleware{handler: handler, delay: chunkDelay}
	} else {
		uc.Handler = handler
	}

	go uc.processEvents()

	return uc, nil
}

// chunkDelayMiddleware wraps a handler and sleeps before each PATCH request
// (TUS chunk upload) to simulate slow network conditions during development.
type chunkDelayMiddleware struct {
	handler http.Handler
	delay   time.Duration
}

func (m *chunkDelayMiddleware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPatch {
		time.Sleep(m.delay)
	}
	m.handler.ServeHTTP(w, r)
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

	// 3. Reject non-gzip files by extension before any data is stored.
	lower := strings.ToLower(fileName)
	if !strings.HasSuffix(lower, ".gz") && !strings.HasSuffix(lower, ".gzip") {
		return errResponse(http.StatusBadRequest,
			"only gzip files are accepted (.gz or .gzip)",
		), tusd.FileInfoChanges{}, errors.New("invalid file extension")
	}

	// 4. Check version exists.
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

	// 5. Validate orthology-specific metadata fields.
	if fileType == "orthology.tsv" {
		if strings.TrimSpace(meta["order"]) == "" {
			return errResponse(http.StatusBadRequest, "orthology.tsv uploads require an \"order\" metadata field"),
				tusd.FileInfoChanges{}, errors.New("missing order metadata")
		}
		if strings.TrimSpace(meta["algorithm"]) == "" {
			return errResponse(http.StatusBadRequest, "orthology.tsv uploads require an \"algorithm\" metadata field"),
				tusd.FileInfoChanges{}, errors.New("missing algorithm metadata")
		}
	}

	// 6. Reject if an active job of the same type already exists for this version.
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
	for event := range uc.tusHandler.CreatedUploads {
		uc.onCreated(event)
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

func (uc *UseCase) handlePreFinish(hook tusd.HookEvent) (tusd.HTTPResponse, error) {
	upload := hook.Upload
	ctx := hook.Context

	srcPath := filepath.Join(uc.uploadDir, upload.ID)

	if ok, err := isGzip(srcPath); err != nil {
		log.Ctx(ctx).Err(err).Str("uploadID", upload.ID).Msg("failed to read uploaded file for gzip check")
		uc.removeUploadFiles(upload.ID)
		_ = uc.uploadRepo.UpdateStatus(ctx, upload.ID, entity.UploadStatusFailed)
		return tusd.HTTPResponse{}, err
	} else if !ok {
		log.Ctx(ctx).Warn().Str("uploadID", upload.ID).Msg("uploaded file is not gzip, discarding")
		uc.removeUploadFiles(upload.ID)
		_ = uc.uploadRepo.UpdateStatus(ctx, upload.ID, entity.UploadStatusFailed)
		return errResponse(http.StatusUnprocessableEntity, "uploaded file is not a valid gzip"), errors.New("not a gzip file")
	}

	version := upload.MetaData["version"]
	fileName := upload.MetaData["fileName"]
	dstDir := filepath.Join(uc.uploadDir, version)
	dstPath := filepath.Join(dstDir, fileName)

	if err := os.MkdirAll(dstDir, 0755); err != nil {
		log.Ctx(ctx).Err(err).Str("dir", dstDir).Msg("failed to create version directory")
		return tusd.HTTPResponse{}, err
	}

	if err := os.Rename(srcPath, dstPath); err != nil {
		log.Ctx(ctx).Err(err).Str("src", srcPath).Str("dst", dstPath).Msg("failed to move upload")
		return tusd.HTTPResponse{}, err
	}

	if err := os.Remove(filepath.Join(uc.uploadDir, upload.ID+".info")); err != nil {
		log.Ctx(ctx).Warn().Err(err).Msg("failed to remove .info file")
	}

	log.Ctx(ctx).Info().Str("uploadID", upload.ID).Str("path", dstPath).Msg("upload complete, file moved")

	jobs, err := uc.enqueueProcessJob(ctx, upload.ID, upload.MetaData, dstPath)
	if err != nil {
		return tusd.HTTPResponse{}, err
	}

	jobIDs := make([]string, len(jobs))
	for i, job := range jobs {
		jobIDs[i] = strconv.FormatUint(job.ID, 10)
	}

	return tusd.HTTPResponse{
		Header: tusd.HTTPHeader{"X-Job-IDs": strings.Join(jobIDs, ",")},
	}, nil
}

func (uc *UseCase) enqueueProcessJob(ctx context.Context, uploadID string, meta tusd.MetaData, filePath string) ([]*entity.Job, error) {
	versionID, err := strconv.ParseUint(meta["_versionID"], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse _versionID for job creation: %w", err)
	}

	fileType := meta["fileType"]

	rawPayload, err := json.Marshal(jobpayload.ProcessPayload{
		UploadFileID: uploadID,
		VersionID:    versionID,
		FilePath:     filePath,
		FileType:     fileType,
		Order:        meta["order"],
		Algorithm:    meta["algorithm"],
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal job payload: %w", err)
	}

	payload := json.RawMessage(rawPayload)
	job := &entity.Job{
		VersionID:     versionID,
		Type:          strings.ToUpper(fileType),
		Payload:       &payload,
		Status:        entity.JobStatusPending,
		MaxRetryCount: uc.maxRetryCount,
	}

	if err := uc.jobRepo.Create(ctx, job); err != nil {
		return nil, fmt.Errorf("failed to create process job: %w", err)
	}

	log.Ctx(ctx).Info().
		Str("uploadID", uploadID).
		Str("jobType", job.Type).
		Uint64("jobID", job.ID).
		Msg("process job enqueued")

	return []*entity.Job{job}, nil
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

func isGzip(filePath string) (bool, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return false, err
	}
	defer f.Close()

	magic := make([]byte, 2)
	if _, err := io.ReadFull(f, magic); err != nil {
		return false, err
	}

	return magic[0] == 0x1f && magic[1] == 0x8b, nil
}

func errResponse(statusCode int, message string) tusd.HTTPResponse {
	return tusd.HTTPResponse{
		StatusCode: statusCode,
		Body:       fmt.Sprintf(`{"message":%q}`, message),
		Header:     tusd.HTTPHeader{"Content-Type": "application/json"},
	}
}
