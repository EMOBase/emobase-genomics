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
	"strconv"
	"strings"
	"time"

	"github.com/EMOBase/emobase-genomics/internal/pkg/auth"
	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	"github.com/EMOBase/emobase-genomics/internal/pkg/jobpayload"
	ucworker "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/worker"
	"github.com/rs/zerolog/log"
	"github.com/tus/tusd/v2/pkg/filelocker"
	"github.com/tus/tusd/v2/pkg/filestore"
	tusd "github.com/tus/tusd/v2/pkg/handler"
)

type UseCase struct {
	// Handler is the HTTP handler to mount in the router. It wraps tusHandler
	// and may inject an artificial chunk delay in development.
	Handler     http.Handler
	tusHandler  *tusd.Handler
	uploadDir   string
	versionRepo IVersionRepository
	jobRepo     IJobRepository
	uploadRepo  IUploadFileRepository
}

func New(
	uploadDir string,
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
		uploadDir:   uploadDir,
		versionRepo: versionRepo,
		jobRepo:     jobRepo,
		uploadRepo:  uploadRepo,
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
		return tusd.HTTPResponse{}, tusd.FileInfoChanges{}, uploadError(http.StatusBadRequest,
			fmt.Sprintf("invalid fileType %q, must be one of: %s", fileType, strings.Join(allowed, ", ")))
	}

	// 2. Validate fileName.
	fileName := meta["fileName"]
	if !fileNamePattern.MatchString(fileName) {
		return tusd.HTTPResponse{}, tusd.FileInfoChanges{}, uploadError(http.StatusBadRequest,
			"invalid fileName: must be 1–255 characters and must not contain path separators or control characters")
	}

	// 3. Reject non-gzip files by extension before any data is stored.
	lower := strings.ToLower(fileName)
	if !strings.HasSuffix(lower, ".gz") && !strings.HasSuffix(lower, ".gzip") {
		return tusd.HTTPResponse{}, tusd.FileInfoChanges{}, uploadError(http.StatusBadRequest,
			"only gzip files are accepted (.gz or .gzip)")
	}

	// Versionless file types: validate the expected filename prefix, then skip
	// all version and job conflict checks.
	if meta, versionless := versionlessFileMeta[fileType]; versionless {
		if !strings.HasPrefix(fileName, meta.prefix) {
			return tusd.HTTPResponse{}, tusd.FileInfoChanges{}, uploadError(http.StatusBadRequest,
				fmt.Sprintf("invalid fileName for fileType %q: must start with %q", fileType, meta.prefix))
		}
		return tusd.HTTPResponse{}, tusd.FileInfoChanges{}, nil
	}

	// 4. Validate orthology-specific metadata fields.
	if fileType == FileTypeOrthologyTSV {
		if strings.TrimSpace(meta["order"]) == "" {
			return tusd.HTTPResponse{}, tusd.FileInfoChanges{}, uploadError(http.StatusBadRequest,
				"orthology.tsv uploads require an \"order\" metadata field")
		}
		if strings.TrimSpace(meta["algorithm"]) == "" {
			return tusd.HTTPResponse{}, tusd.FileInfoChanges{}, uploadError(http.StatusBadRequest,
				"orthology.tsv uploads require an \"algorithm\" metadata field")
		}
	}

	// 5. Check version exists.
	version, err := uc.versionRepo.FindByName(hook.Context, meta["version"])
	if err != nil {
		log.Ctx(hook.Context).Err(err).Msg("version lookup failed in pre-upload hook")
		return tusd.HTTPResponse{}, tusd.FileInfoChanges{}, err
	}
	if version == nil {
		return tusd.HTTPResponse{}, tusd.FileInfoChanges{}, uploadError(http.StatusBadRequest,
			fmt.Sprintf("version %q not found", meta["version"]))
	}

	// 6. Reject if an active job of the same type already exists for this version.
	hasActive, err := uc.jobRepo.HasActiveJobOfType(hook.Context, version.ID, fileType)
	if err != nil {
		log.Ctx(hook.Context).Err(err).Msg("job lookup failed in pre-upload hook")
		return tusd.HTTPResponse{}, tusd.FileInfoChanges{}, err
	}
	if hasActive {
		return tusd.HTTPResponse{}, tusd.FileInfoChanges{}, uploadError(http.StatusConflict,
			fmt.Sprintf("a job for file type %q is already pending or running for this version", fileType))
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

	// Versionless uploads are not tracked in upload_files.
	if _, versionless := versionlessFileTypes[upload.MetaData["fileType"]]; versionless {
		log.Info().
			Str("uploadID", upload.ID).
			Str("fileType", upload.MetaData["fileType"]).
			Msg("versionless upload started")
		return
	}

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
		FilePath:     filepath.Join(upload.MetaData["version"], filepath.Base(upload.MetaData["fileName"])),
		FileType:     upload.MetaData["fileType"],
		FileSize:     upload.Size,
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
	fileType := upload.MetaData["fileType"]
	fileName := upload.MetaData["fileName"]
	_, versionless := versionlessFileTypes[fileType]

	if ok, err := isGzip(srcPath); err != nil {
		log.Ctx(ctx).Err(err).Str("uploadID", upload.ID).Msg("failed to read uploaded file for gzip check")
		uc.removeUploadFiles(upload.ID)
		if !versionless {
			_ = uc.uploadRepo.UpdateStatus(ctx, upload.ID, entity.UploadStatusFailed)
		}
		return tusd.HTTPResponse{}, err
	} else if !ok {
		log.Ctx(ctx).Warn().Str("uploadID", upload.ID).Msg("uploaded file is not gzip, discarding")
		uc.removeUploadFiles(upload.ID)
		if !versionless {
			_ = uc.uploadRepo.UpdateStatus(ctx, upload.ID, entity.UploadStatusFailed)
		}
		return tusd.HTTPResponse{}, uploadError(http.StatusUnprocessableEntity, "uploaded file is not a valid gzip")
	}

	// Versionless files go to the uploads root under a canonical fixed name;
	// versioned files go into a version subfolder.
	var dstDir, dstPath string
	if versionless {
		dstDir = uc.uploadDir
		dstPath = filepath.Join(dstDir, versionlessFileMeta[fileType].canonicalName)
	} else {
		version := upload.MetaData["version"]
		dstDir = filepath.Join(uc.uploadDir, version)
		dstPath = filepath.Join(dstDir, filepath.Base(fileName))
	}

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

	// Versionless files require no further processing.
	if versionless {
		return tusd.HTTPResponse{}, nil
	}

	jobIDs, err := uc.enqueueProcessJob(ctx, upload.ID, upload.MetaData, dstPath)
	if err != nil {
		return tusd.HTTPResponse{}, err
	}

	idStrs := make([]string, len(jobIDs))
	for i, id := range jobIDs {
		idStrs[i] = strconv.FormatUint(id, 10)
	}

	return tusd.HTTPResponse{
		Header: tusd.HTTPHeader{"X-Job-IDs": strings.Join(idStrs, ",")},
	}, nil
}

func (uc *UseCase) enqueueProcessJob(ctx context.Context, uploadID string, meta tusd.MetaData, filePath string) ([]uint64, error) {
	versionID, err := strconv.ParseUint(meta["_versionID"], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse _versionID for job creation: %w", err)
	}

	fileType := meta["fileType"]

	// genomic.fna has no processing step; no job is enqueued on upload.
	if fileType == FileTypeGenomicFNA {
		return []uint64{}, nil
	}

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
	jobType := strings.ToUpper(fileType)
	job := &entity.Job{
		VersionID:   versionID,
		FileID:      &uploadID,
		Type:        jobType,
		Description: ucworker.JobDescriptions[jobType],
		Payload:     &payload,
		Status:      entity.JobStatusPending,
	}

	if err := uc.jobRepo.Create(ctx, job); err != nil {
		return nil, fmt.Errorf("failed to create process job: %w", err)
	}

	log.Ctx(ctx).Info().
		Str("uploadID", uploadID).
		Str("jobType", job.Type).
		Uint64("jobID", job.ID).
		Msg("process job enqueued")

	jobIDs := []uint64{job.ID}

	if fileType == FileTypeGenomicGFF {
		// Also enqueue a SYNONYM job that combines GFF3 + versionless FB synonym files.
		// TODO: Refactor this to process only the GFF file instead.
		synonymID, err := uc.enqueueSynonymJob(ctx, versionID, uploadID, filePath)
		if err != nil {
			return nil, err
		}
		jobIDs = append(jobIDs, synonymID)
	}

	return jobIDs, nil
}

// enqueueSynonymJob creates a single SYNONYM job that carries the GFF3 file
// path plus any versionless FB synonym files found in the uploads root.
func (uc *UseCase) enqueueSynonymJob(ctx context.Context, versionID uint64, uploadFileID, gffFilePath string) (uint64, error) {
	var synonymFiles []string
	for _, name := range []string{"fb_synonym.tsv.gz", "fbgn_fbtr_fbpp.tsv.gz"} {
		p := filepath.Join(uc.uploadDir, name)
		if _, err := os.Stat(p); err == nil {
			synonymFiles = append(synonymFiles, p)
		}
	}

	rawPayload, err := json.Marshal(jobpayload.ProcessPayload{
		VersionID:    versionID,
		FilePath:     gffFilePath,
		FileType:     "synonym",
		SynonymFiles: synonymFiles,
	})
	if err != nil {
		return 0, fmt.Errorf("failed to marshal synonym job payload: %w", err)
	}

	p := json.RawMessage(rawPayload)
	j := &entity.Job{
		VersionID:   versionID,
		FileID:      &uploadFileID,
		Type:        ucworker.JobTypeGenomicGFFSynonym,
		Description: ucworker.JobDescriptions[ucworker.JobTypeGenomicGFFSynonym],
		Payload:     &p,
		Status:      entity.JobStatusPending,
	}

	if err := uc.jobRepo.Create(ctx, j); err != nil {
		return 0, fmt.Errorf("failed to create synonym job: %w", err)
	}

	log.Ctx(ctx).Info().
		Uint64("jobID", j.ID).
		Str("gffFile", gffFilePath).
		Strs("synonymFiles", synonymFiles).
		Msg("synonym job enqueued")

	return j.ID, nil
}

var ErrUploadFileNotFound = errors.New("upload file not found")
var ErrUploadFileNotDeletable = errors.New("only orthology.tsv files can be deleted")

func (uc *UseCase) DeleteFile(ctx context.Context, id string, deletedBy string) (*entity.Job, error) {
	f, err := uc.uploadRepo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if f == nil {
		return nil, ErrUploadFileNotFound
	}
	if f.FileType != FileTypeOrthologyTSV {
		return nil, ErrUploadFileNotDeletable
	}

	rawPayload, err := json.Marshal(jobpayload.DeleteFilePayload{
		UploadFileID: id,
		DeletedBy:    deletedBy,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal delete file payload: %w", err)
	}

	p := json.RawMessage(rawPayload)
	job := &entity.Job{
		VersionID:   f.VersionID,
		FileID:      &id,
		Type:        ucworker.JobTypeOrthologyTSVDelete,
		Description: ucworker.JobDescriptions[ucworker.JobTypeOrthologyTSVDelete],
		Payload:     &p,
		Status:      entity.JobStatusPending,
	}

	if err := uc.jobRepo.Create(ctx, job); err != nil {
		return nil, err
	}
	return job, nil
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
	defer func() { _ = f.Close() }()

	magic := make([]byte, 2)
	if _, err := io.ReadFull(f, magic); err != nil {
		return false, err
	}

	return magic[0] == 0x1f && magic[1] == 0x8b, nil
}

// uploadError returns a tusd.Error whose HTTPResponse carries the given status
// code and a JSON body. tusd's sendError only honours the status code when the
// error satisfies errors.As(err, &tusd.Error{}), so returning plain errors.New
// always produces a 500 — this helper fixes that.
func uploadError(statusCode int, message string) tusd.Error {
	return tusd.NewError(
		http.StatusText(statusCode),
		message,
		statusCode,
	)
}
