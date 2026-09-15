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
	"github.com/rs/zerolog/log"
	"github.com/tus/tusd/v2/pkg/filelocker"
	"github.com/tus/tusd/v2/pkg/filestore"
	tusd "github.com/tus/tusd/v2/pkg/handler"
)

type UseCase struct {
	Handler             http.Handler
	tusHandler          *tusd.Handler
	uploadDir           string
	geneLinkBase        string
	mainSpecies         string
	versionRepo         IVersionRepository
	assemblyVersionRepo IAssemblyVersionRepository
	jobRepo             IJobRepository
	uploadRepo          IUploadFileRepository
}

func New(
	uploadDir string,
	tusBasePath string,
	geneLinkBase string,
	mainSpecies string,
	staleUploadAge time.Duration,
	versionRepo IVersionRepository,
	assemblyVersionRepo IAssemblyVersionRepository,
	jobRepo IJobRepository,
	uploadRepo IUploadFileRepository,
) (*UseCase, error) {
	store := filestore.New(uploadDir)
	locker := filelocker.New(uploadDir)

	composer := tusd.NewStoreComposer()
	store.UseIn(composer)
	locker.UseIn(composer)

	uc := &UseCase{
		uploadDir:           uploadDir,
		geneLinkBase:        geneLinkBase,
		mainSpecies:         mainSpecies,
		versionRepo:         versionRepo,
		assemblyVersionRepo: assemblyVersionRepo,
		jobRepo:             jobRepo,
		uploadRepo:          uploadRepo,
	}

	handler, err := tusd.NewHandler(tusd.Config{
		BasePath:                  tusBasePath,
		StoreComposer:             composer,
		DisableDownload:           true,
		NotifyCreatedUploads:      true,
		PreUploadCreateCallback:   uc.handlePreUploadCreate,
		PreFinishResponseCallback: uc.handlePreFinish,
		RespectForwardedHeaders:   true,
	})
	if err != nil {
		log.Error().Err(err).Msg("unable to create tusd handler")
		return nil, err
	}

	uc.tusHandler = handler
	uc.Handler = handler

	go uc.processEvents()

	if staleUploadAge > 0 {
		go uc.runStaleCleanup(staleUploadAge)
	}

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

	// 4. Validate file-type-specific metadata fields.
	if fileType == entity.FileTypeOrthologyTSV {
		if strings.TrimSpace(meta["order"]) == "" {
			return tusd.HTTPResponse{}, tusd.FileInfoChanges{}, uploadError(http.StatusBadRequest,
				"orthology.tsv uploads require an \"order\" metadata field")
		}
		if _, err := strconv.Atoi(meta["order"]); err != nil {
			return tusd.HTTPResponse{}, tusd.FileInfoChanges{}, uploadError(http.StatusBadRequest,
				"orthology.tsv \"order\" metadata field must be an integer")
		}
		if strings.TrimSpace(meta["algorithm"]) == "" {
			return tusd.HTTPResponse{}, tusd.FileInfoChanges{}, uploadError(http.StatusBadRequest,
				"orthology.tsv uploads require an \"algorithm\" metadata field")
		}
	}
	if fileType == entity.FileTypeGenomicGFF {
		if strings.TrimSpace(meta["geneIDKey"]) == "" {
			return tusd.HTTPResponse{}, tusd.FileInfoChanges{}, uploadError(http.StatusBadRequest,
				"genomic.gff uploads require a \"geneIDKey\" metadata field")
		}
		if _, err := strconv.Atoi(meta["trimPrefixChars"]); err != nil {
			return tusd.HTTPResponse{}, tusd.FileInfoChanges{}, uploadError(http.StatusBadRequest,
				"genomic.gff \"trimPrefixChars\" metadata field must be an integer")
		}
		if _, err := strconv.Atoi(meta["trimSuffixChars"]); err != nil {
			return tusd.HTTPResponse{}, tusd.FileInfoChanges{}, uploadError(http.StatusBadRequest,
				"genomic.gff \"trimSuffixChars\" metadata field must be an integer")
		}
	}
	if fileType == entity.FileTypeJBrowseTrack {
		if strings.TrimSpace(meta["trackName"]) == "" {
			return tusd.HTTPResponse{}, tusd.FileInfoChanges{}, uploadError(http.StatusBadRequest,
				"jbrowse.track uploads require a \"trackName\" metadata field")
		}
	}
	if fileType == entity.FileTypeSpeciesSynonym {
		if strings.TrimSpace(meta["species"]) == "" {
			return tusd.HTTPResponse{}, tusd.FileInfoChanges{}, uploadError(http.StatusBadRequest,
				"species.synonym uploads require a \"species\" metadata field")
		}
	}

	// 5. Every file type requires an "assembly" metadata field (the target
	// Assembly Version's species code) except orthology.tsv, which is shared
	// across every species in the Database Version rather than owned by one.
	if fileType != entity.FileTypeOrthologyTSV && strings.TrimSpace(meta["assembly"]) == "" {
		return tusd.HTTPResponse{}, tusd.FileInfoChanges{}, uploadError(http.StatusBadRequest,
			fmt.Sprintf("%q uploads require an \"assembly\" metadata field", fileType))
	}

	// 6. Check version exists.
	version, err := uc.versionRepo.FindByName(hook.Context, meta["version"])
	if err != nil {
		log.Ctx(hook.Context).Err(err).Msg("version lookup failed in pre-upload hook")
		return tusd.HTTPResponse{}, tusd.FileInfoChanges{}, err
	}
	if version == nil {
		return tusd.HTTPResponse{}, tusd.FileInfoChanges{}, uploadError(http.StatusBadRequest,
			fmt.Sprintf("version %q not found", meta["version"]))
	}

	// 7. Resolve the Assembly Version (species) this upload belongs to, unless
	// this is a shared orthology.tsv upload.
	var assemblyVersion *entity.AssemblyVersion
	if fileType != entity.FileTypeOrthologyTSV {
		assemblyVersion, err = uc.assemblyVersionRepo.FindBySpecies(hook.Context, version.ID, meta["assembly"])
		if err != nil {
			log.Ctx(hook.Context).Err(err).Msg("assembly version lookup failed in pre-upload hook")
			return tusd.HTTPResponse{}, tusd.FileInfoChanges{}, err
		}
		if assemblyVersion == nil {
			return tusd.HTTPResponse{}, tusd.FileInfoChanges{}, uploadError(http.StatusBadRequest,
				fmt.Sprintf("assembly %q not found in version %q", meta["assembly"], meta["version"]))
		}
	}

	// 8. Species-restricted file types — scoped to the resolved assembly's own
	// species (assemblyVersion is always non-nil here since dsrna.csv is not
	// orthology.tsv, so it always goes through step 7 above).
	if fileType == entity.FileTypeDsRNACSV && assemblyVersion.Species != entity.SpeciesTcas {
		return tusd.HTTPResponse{}, tusd.FileInfoChanges{}, uploadError(http.StatusBadRequest,
			"dsrna.csv uploads are only supported for the \"Tcas\" species")
	}

	// 9. Reject if an active job of the same type already exists for this
	// assembly — or, for the shared orthology.tsv, for the whole Database
	// Version, exactly as before. jbrowse.track is exempt: a version may have
	// multiple tracks processing concurrently.
	if fileType != entity.FileTypeJBrowseTrack {
		var hasActive bool
		if fileType == entity.FileTypeOrthologyTSV {
			hasActive, err = uc.jobRepo.HasActiveJobOfType(hook.Context, version.ID, fileType)
		} else {
			hasActive, err = uc.jobRepo.HasActiveJobOfTypeForAssemblyVersion(hook.Context, assemblyVersion.ID, fileType)
		}
		if err != nil {
			log.Ctx(hook.Context).Err(err).Msg("job lookup failed in pre-upload hook")
			return tusd.HTTPResponse{}, tusd.FileInfoChanges{}, err
		}
		if hasActive {
			return tusd.HTTPResponse{}, tusd.FileInfoChanges{}, uploadError(http.StatusConflict,
				fmt.Sprintf("a job for file type %q is already pending or running for this version", fileType))
		}
	}

	// Propagate versionID/assemblyVersionID through metadata so the
	// CreatedUploads handler can use them without a second DB roundtrip.
	newMeta := make(tusd.MetaData, len(meta)+3)
	maps.Copy(newMeta, meta)
	newMeta["_versionID"] = strconv.FormatUint(version.ID, 10)
	if assemblyVersion != nil {
		newMeta["_assemblyVersionID"] = strconv.FormatUint(assemblyVersion.ID, 10)
		newMeta["_assemblySpecies"] = assemblyVersion.Species
		newMeta["_assemblyID"] = assemblyVersion.AssemblyID()
	}

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

	assemblyVersionID, err := parseOptionalAssemblyVersionID(upload.MetaData)
	if err != nil {
		log.Error().Err(err).Str("uploadID", upload.ID).Msg("invalid _assemblyVersionID in upload metadata")
		uc.removeUploadFiles(upload.ID)
		return
	}

	creator := auth.UsernameFromContext(event.Context)

	dstDir := uc.uploadDirFor(upload.MetaData["version"], upload.MetaData["_assemblySpecies"])
	f := &entity.UploadFile{
		ID:                upload.ID,
		VersionID:         versionID,
		AssemblyVersionID: assemblyVersionID,
		FilePath:          filepath.Join(dstDir, filepath.Base(upload.MetaData["fileName"])),
		FileType:          upload.MetaData["fileType"],
		FileSize:          upload.Size,
		UploadStatus:      entity.UploadStatusUploading,
		CreatedBy:         creator,
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
	fileName := upload.MetaData["fileName"]

	if ok, err := isGzip(srcPath); err != nil {
		log.Ctx(ctx).Err(err).Str("uploadID", upload.ID).Msg("failed to read uploaded file for gzip check")
		uc.removeUploadFiles(upload.ID)
		_ = uc.uploadRepo.UpdateStatus(ctx, upload.ID, entity.UploadStatusFailed)
		return tusd.HTTPResponse{}, err
	} else if !ok {
		log.Ctx(ctx).Warn().Str("uploadID", upload.ID).Msg("uploaded file is not gzip, discarding")
		uc.removeUploadFiles(upload.ID)
		_ = uc.uploadRepo.UpdateStatus(ctx, upload.ID, entity.UploadStatusFailed)
		return tusd.HTTPResponse{}, uploadError(http.StatusUnprocessableEntity, "uploaded file is not a valid gzip")
	}

	version := upload.MetaData["version"]
	dstDir := uc.uploadDirFor(version, upload.MetaData["_assemblySpecies"])
	dstPath := filepath.Join(dstDir, filepath.Base(fileName))

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

	if err := uc.uploadRepo.UpdateStatus(ctx, upload.ID, entity.UploadStatusCompleted); err != nil {
		log.Ctx(ctx).Warn().Err(err).Str("uploadID", upload.ID).Msg("failed to mark upload file as completed")
	}

	idStrs := make([]string, len(jobs))
	for i, j := range jobs {
		idStrs[i] = strconv.FormatUint(j.ID, 10)
	}

	jobsJSON, err := json.Marshal(jobs)
	if err != nil {
		return tusd.HTTPResponse{}, fmt.Errorf("failed to marshal jobs response: %w", err)
	}

	return tusd.HTTPResponse{
		Header: tusd.HTTPHeader{
			"X-Job-IDs": strings.Join(idStrs, ","), // deprecated: use X-Jobs instead
			"X-Jobs":    string(jobsJSON),
		},
	}, nil
}

func (uc *UseCase) enqueueProcessJob(ctx context.Context, uploadID string, meta tusd.MetaData, filePath string) ([]entity.Job, error) {
	versionID, err := strconv.ParseUint(meta["_versionID"], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse _versionID for job creation: %w", err)
	}

	assemblyVersionID, err := parseOptionalAssemblyVersionID(meta)
	if err != nil {
		return nil, err
	}
	assemblySpecies := meta["_assemblySpecies"]
	assemblyID := meta["_assemblyID"]

	fileType := meta["fileType"]

	// genomic.fna has no parsing step; enqueue only the JBrowse2 assembly setup.
	if fileType == entity.FileTypeGenomicFNA {
		job, err := uc.enqueueFNASetupJBrowse2Job(ctx, versionID, assemblyVersionID, uploadID, meta["version"], assemblyID, filePath)
		if err != nil {
			return nil, err
		}
		return []entity.Job{job}, nil
	}

	// species_synonym: parse a species-specific FB synonym file.
	if fileType == entity.FileTypeSpeciesSynonym {
		rawPayload, err := json.Marshal(jobpayload.SpeciesSynonymPayload{
			UploadFileID: uploadID,
			VersionID:    versionID,
			FilePath:     filePath,
			Species:      strings.TrimSpace(meta["species"]),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to marshal %s payload: %w", entity.JobTypeSpeciesSynonym, err)
		}
		p := json.RawMessage(rawPayload)
		now := time.Now().UTC()
		job := &entity.Job{
			VersionID:         versionID,
			AssemblyVersionID: assemblyVersionID,
			FileID:            &uploadID,
			Type:              entity.JobTypeSpeciesSynonym,
			Description:       entity.JobDescriptions[entity.JobTypeSpeciesSynonym],
			Payload:           &p,
			Status:            entity.JobStatusPending,
			CreatedAt:         now,
			UpdatedAt:         now,
		}
		if err := uc.jobRepo.Create(ctx, job); err != nil {
			return nil, fmt.Errorf("failed to create %s job: %w", entity.JobTypeSpeciesSynonym, err)
		}
		log.Ctx(ctx).Info().
			Str("uploadID", uploadID).
			Uint64("jobID", job.ID).
			Msg("species.synonym job enqueued")
		return []entity.Job{*job}, nil
	}

	// jbrowse.track: run jbrowse add-track with track-specific metadata.
	if fileType == entity.FileTypeJBrowseTrack {
		selectInDefaultSession, _ := strconv.ParseBool(meta["selectInDefaultSession"])
		rawPayload, err := json.Marshal(jobpayload.JBrowseTrackPayload{
			VersionName:            meta["version"],
			AssemblyID:             assemblyID,
			FilePath:               filePath,
			TrackName:              strings.TrimSpace(meta["trackName"]),
			FileID:                 uploadID,
			Category:               strings.TrimSpace(meta["category"]),
			SelectInDefaultSession: selectInDefaultSession,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to marshal %s payload: %w", entity.JobTypeJBrowseTrack, err)
		}
		p := json.RawMessage(rawPayload)
		now := time.Now().UTC()
		job := &entity.Job{
			VersionID:         versionID,
			AssemblyVersionID: assemblyVersionID,
			FileID:            &uploadID,
			Type:              entity.JobTypeJBrowseTrack,
			Description:       entity.JobDescriptions[entity.JobTypeJBrowseTrack],
			Payload:           &p,
			Status:            entity.JobStatusPending,
			CreatedAt:         now,
			UpdatedAt:         now,
		}
		if err := uc.jobRepo.Create(ctx, job); err != nil {
			return nil, fmt.Errorf("failed to create %s job: %w", entity.JobTypeJBrowseTrack, err)
		}
		log.Ctx(ctx).Info().
			Str("uploadID", uploadID).
			Uint64("jobID", job.ID).
			Msg("jbrowse.track job enqueued")
		return []entity.Job{*job}, nil
	}

	var rawPayload []byte
	switch fileType {
	case entity.FileTypeGenomicGFF:
		trimPrefixChars, _ := strconv.Atoi(meta["trimPrefixChars"])
		trimSuffixChars, _ := strconv.Atoi(meta["trimSuffixChars"])
		rawPayload, err = json.Marshal(jobpayload.GenomicGFFPayload{
			UploadFileID:    uploadID,
			VersionID:       versionID,
			FilePath:        filePath,
			Species:         assemblySpecies,
			GeneIDKey:       strings.TrimSpace(meta["geneIDKey"]),
			TrimPrefixChars: trimPrefixChars,
			TrimSuffixChars: trimSuffixChars,
			OldGeneIDKeys:   parseCommaSeparated(meta["oldGeneIDKeys"]),
		})
	case entity.FileTypeOrthologyTSV:
		order, _ := strconv.Atoi(meta["order"])
		rawPayload, err = json.Marshal(jobpayload.OrthologyTSVPayload{
			UploadFileID: uploadID,
			VersionID:    versionID,
			FilePath:     filePath,
			Order:        order,
			Algorithm:    strings.TrimSpace(meta["algorithm"]),
		})
	default:
		rawPayload, err = json.Marshal(jobpayload.ProcessPayload{
			UploadFileID: uploadID,
			VersionID:    versionID,
			FilePath:     filePath,
			Species:      assemblySpecies,
		})
	}
	if err != nil {
		return nil, fmt.Errorf("failed to marshal job payload: %w", err)
	}

	payload := json.RawMessage(rawPayload)
	jobType := strings.ToUpper(fileType)
	now := time.Now().UTC()
	job := &entity.Job{
		VersionID:         versionID,
		AssemblyVersionID: assemblyVersionID,
		FileID:            &uploadID,
		Type:              jobType,
		Description:       entity.JobDescriptions[jobType],
		Payload:           &payload,
		Status:            entity.JobStatusPending,
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	if err := uc.jobRepo.Create(ctx, job); err != nil {
		return nil, fmt.Errorf("failed to create process job: %w", err)
	}

	log.Ctx(ctx).Info().
		Str("uploadID", uploadID).
		Str("jobType", job.Type).
		Uint64("jobID", job.ID).
		Msg("process job enqueued")

	jobs := []entity.Job{*job}

	if fileType == entity.FileTypeGenomicGFF {
		trimPrefixChars, _ := strconv.Atoi(meta["trimPrefixChars"])
		trimSuffixChars, _ := strconv.Atoi(meta["trimSuffixChars"])
		synonymJob, err := uc.enqueueSpeciesSynonymJob(ctx, versionID, assemblyVersionID, uploadID, filePath,
			assemblySpecies, strings.TrimSpace(meta["geneIDKey"]),
			trimPrefixChars, trimSuffixChars, parseCommaSeparated(meta["oldGeneIDKeys"]))
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, synonymJob)

		// If GENOMIC.FNA:SETUP_JBROWSE2 is already done, enqueue GFF setup immediately.
		if gffSetupJob, err := uc.tryEnqueueGFFSetupJBrowse2(ctx, versionID, assemblyVersionID, meta["version"], assemblyID, uploadID, filePath); err != nil {
			log.Ctx(ctx).Warn().Err(err).Msgf("failed to check/enqueue %s after %s upload", entity.JobTypeGenomicGFFSetupJBrowse2, entity.JobTypeGenomicGFF)
		} else if gffSetupJob != nil {
			jobs = append(jobs, *gffSetupJob)
		}
	}

	return jobs, nil
}

func (uc *UseCase) enqueueSpeciesSynonymJob(ctx context.Context, versionID uint64, assemblyVersionID *uint64, uploadID, filePath, species, geneIDKey string, trimPrefixChars, trimSuffixChars int, oldGeneIDKeys []string) (entity.Job, error) {
	rawPayload, err := json.Marshal(jobpayload.SpeciesSynonymPayload{
		UploadFileID:    uploadID,
		VersionID:       versionID,
		FilePath:        filePath,
		Species:         species,
		GeneIDKey:       geneIDKey,
		TrimPrefixChars: trimPrefixChars,
		TrimSuffixChars: trimSuffixChars,
		OldGeneIDKeys:   oldGeneIDKeys,
	})
	if err != nil {
		return entity.Job{}, fmt.Errorf("failed to marshal %s payload: %w", entity.JobTypeSpeciesSynonym, err)
	}

	p := json.RawMessage(rawPayload)
	now := time.Now().UTC()
	j := &entity.Job{
		VersionID:         versionID,
		AssemblyVersionID: assemblyVersionID,
		FileID:            &uploadID,
		Type:              entity.JobTypeSpeciesSynonym,
		Description:       entity.JobDescriptions[entity.JobTypeSpeciesSynonym],
		Payload:           &p,
		Status:            entity.JobStatusPending,
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	if err := uc.jobRepo.Create(ctx, j); err != nil {
		return entity.Job{}, fmt.Errorf("failed to create %s job: %w", entity.JobTypeSpeciesSynonym, err)
	}

	log.Ctx(ctx).Info().
		Uint64("jobID", j.ID).
		Str("species", species).
		Msgf("%s job enqueued", entity.JobTypeSpeciesSynonym)

	return *j, nil
}

func (uc *UseCase) enqueueFNASetupJBrowse2Job(ctx context.Context, versionID uint64, assemblyVersionID *uint64, uploadID, versionName, assemblyID, filePath string) (entity.Job, error) {
	rawPayload, err := json.Marshal(jobpayload.SetupJBrowse2FNAPayload{
		VersionName:    versionName,
		AssemblyID:     assemblyID,
		GenomicFNAPath: filePath,
	})
	if err != nil {
		return entity.Job{}, fmt.Errorf("failed to marshal %s payload: %w", entity.JobTypeGenomicFNASetupJBrowse2, err)
	}

	p := json.RawMessage(rawPayload)
	now := time.Now().UTC()
	j := &entity.Job{
		VersionID:         versionID,
		AssemblyVersionID: assemblyVersionID,
		FileID:            &uploadID,
		Type:              entity.JobTypeGenomicFNASetupJBrowse2,
		Description:       entity.JobDescriptions[entity.JobTypeGenomicFNASetupJBrowse2],
		Payload:           &p,
		Status:            entity.JobStatusPending,
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	if err := uc.jobRepo.Create(ctx, j); err != nil {
		return entity.Job{}, fmt.Errorf("failed to create %s job: %w", entity.JobTypeGenomicFNASetupJBrowse2, err)
	}

	log.Ctx(ctx).Info().
		Uint64("jobID", j.ID).
		Str("version", versionName).
		Msgf("%s job enqueued", entity.JobTypeGenomicFNASetupJBrowse2)

	return *j, nil
}

// tryEnqueueGFFSetupJBrowse2 creates a GENOMIC.GFF:SETUP_JBROWSE2 job if
// GENOMIC.FNA:SETUP_JBROWSE2 is done and no non-failed job exists for this GFF file.
// GeneIDKey is read from the GENOMIC.GFF job's payload to keep a single source of truth.
func (uc *UseCase) tryEnqueueGFFSetupJBrowse2(ctx context.Context, versionID uint64, assemblyVersionID *uint64, versionName, assemblyID, gffFileID, gffFilePath string) (*entity.Job, error) {
	if assemblyVersionID == nil {
		return nil, nil
	}
	fnaFile, err := uc.uploadRepo.FindLatestCompletedByAssemblyVersionAndType(ctx, *assemblyVersionID, entity.FileTypeGenomicFNA)
	if err != nil {
		return nil, fmt.Errorf("failed to find latest %s file: %w", entity.FileTypeGenomicFNA, err)
	}
	if fnaFile == nil {
		return nil, nil
	}
	done, err := uc.jobRepo.HasDoneJobOfTypeForFile(ctx, fnaFile.ID, entity.JobTypeGenomicFNASetupJBrowse2)
	if err != nil {
		return nil, fmt.Errorf("failed to check %s status: %w", entity.JobTypeGenomicFNASetupJBrowse2, err)
	}
	if !done {
		return nil, nil
	}

	exists, err := uc.jobRepo.HasNonFailedJobOfTypeForFile(ctx, gffFileID, entity.JobTypeGenomicGFFSetupJBrowse2)
	if err != nil {
		return nil, fmt.Errorf("failed to check existing %s job: %w", entity.JobTypeGenomicGFFSetupJBrowse2, err)
	}
	if exists {
		return nil, nil
	}

	gffJob, err := uc.jobRepo.FindLatestByFileAndType(ctx, gffFileID, entity.JobTypeGenomicGFF)
	if err != nil {
		return nil, fmt.Errorf("failed to look up %s job for file: %w", entity.JobTypeGenomicGFF, err)
	}
	var geneIDKey string
	var trimPrefixChars, trimSuffixChars int
	if gffJob != nil && gffJob.Payload != nil {
		var p jobpayload.GenomicGFFPayload
		if err := json.Unmarshal(*gffJob.Payload, &p); err == nil {
			geneIDKey = p.GeneIDKey
			trimPrefixChars = p.TrimPrefixChars
			trimSuffixChars = p.TrimSuffixChars
		}
	}

	rawPayload, err := json.Marshal(jobpayload.SetupJBrowse2GFFPayload{
		VersionName:     versionName,
		AssemblyID:      assemblyID,
		GenomicGFFPath:  gffFilePath,
		GeneIDKey:       geneIDKey,
		GeneLinkBase:    uc.geneLinkBase,
		TrimPrefixChars: trimPrefixChars,
		TrimSuffixChars: trimSuffixChars,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal %s payload: %w", entity.JobTypeGenomicGFFSetupJBrowse2, err)
	}

	p := json.RawMessage(rawPayload)
	now := time.Now().UTC()
	j := &entity.Job{
		VersionID:         versionID,
		AssemblyVersionID: assemblyVersionID,
		FileID:            &gffFileID,
		Type:              entity.JobTypeGenomicGFFSetupJBrowse2,
		Description:       entity.JobDescriptions[entity.JobTypeGenomicGFFSetupJBrowse2],
		Payload:           &p,
		Status:            entity.JobStatusPending,
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	if err := uc.jobRepo.Create(ctx, j); err != nil {
		return nil, fmt.Errorf("failed to create %s job: %w", entity.JobTypeGenomicGFFSetupJBrowse2, err)
	}

	log.Ctx(ctx).Info().
		Uint64("jobID", j.ID).
		Str("gffFileID", gffFileID).
		Msgf("%s job enqueued", entity.JobTypeGenomicGFFSetupJBrowse2)

	return j, nil
}

var ErrUploadFileNotFound = errors.New("upload file not found")
var ErrUploadFileNotDeletable = errors.New("this file type does not support deletion")
var ErrUploadFileDeletePending = errors.New("a delete job for this file is already pending or running")
var ErrVersionNotFound = errors.New("version not found")

// UploadFileSummary is the API-facing representation of an upload file.
type UploadFileSummary struct {
	ID           string              `json:"id"`
	VersionID    uint64              `json:"versionId"`
	FileType     string              `json:"fileType"`
	FileSize     int64               `json:"fileSize"`
	UploadStatus entity.UploadStatus `json:"uploadStatus"`
	CreatedAt    time.Time           `json:"createdAt"`
	CreatedBy    string              `json:"createdBy"`
	CompletedAt  *time.Time          `json:"completedAt,omitempty"`
}

func (uc *UseCase) ListByVersion(ctx context.Context, versionName string) ([]UploadFileSummary, error) {
	v, err := uc.versionRepo.FindByName(ctx, versionName)
	if err != nil {
		return nil, err
	}
	if v == nil {
		return nil, ErrVersionNotFound
	}

	files, err := uc.uploadRepo.ListByVersionID(ctx, v.ID)
	if err != nil {
		return nil, err
	}

	summaries := make([]UploadFileSummary, len(files))
	for i, f := range files {
		summaries[i] = UploadFileSummary{
			ID:           f.ID,
			VersionID:    f.VersionID,
			FileType:     f.FileType,
			FileSize:     f.FileSize,
			UploadStatus: f.UploadStatus,
			CreatedAt:    f.CreatedAt,
			CreatedBy:    f.CreatedBy,
			CompletedAt:  f.CompletedAt,
		}
	}
	return summaries, nil
}

// deletableFileTypes maps each file type that supports deletion to its delete job type.
var deletableFileTypes = map[string]string{
	entity.FileTypeOrthologyTSV:   entity.JobTypeOrthologyTSVDelete,
	entity.FileTypeJBrowseTrack:   entity.JobTypeJBrowseTrackDelete,
	entity.FileTypeSpeciesSynonym: entity.JobTypeSpeciesSynonymDelete,
}

func (uc *UseCase) DeleteFile(ctx context.Context, id string, deletedBy string) (*entity.Job, error) {
	f, err := uc.uploadRepo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if f == nil {
		return nil, ErrUploadFileNotFound
	}
	deleteJobType, ok := deletableFileTypes[f.FileType]
	if !ok {
		return nil, ErrUploadFileNotDeletable
	}

	hasActive, err := uc.jobRepo.HasActiveJobOfTypeForFile(ctx, id, deleteJobType)
	if err != nil {
		return nil, err
	}
	if hasActive {
		return nil, ErrUploadFileDeletePending
	}

	rawPayload, err := json.Marshal(jobpayload.DeleteFilePayload{
		UploadFileID: id,
		DeletedBy:    deletedBy,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal delete file payload: %w", err)
	}

	p := json.RawMessage(rawPayload)
	now := time.Now().UTC()
	job := &entity.Job{
		VersionID:         f.VersionID,
		AssemblyVersionID: f.AssemblyVersionID,
		FileID:            &id,
		Type:              deleteJobType,
		Description:       entity.JobDescriptions[deleteJobType],
		Payload:           &p,
		Status:            entity.JobStatusPending,
		CreatedAt:         now,
		UpdatedAt:         now,
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

func (uc *UseCase) runStaleCleanup(maxAge time.Duration) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		uc.cleanStaleUploads(maxAge)
	}
}

func (uc *UseCase) cleanStaleUploads(maxAge time.Duration) {
	entries, err := os.ReadDir(uc.uploadDir)
	if err != nil {
		log.Error().Err(err).Msg("stale upload cleanup: failed to read upload dir")
		return
	}

	cutoff := time.Now().Add(-maxAge)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".info") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(cutoff) {
			continue
		}

		uploadID := strings.TrimSuffix(entry.Name(), ".info")
		log.Warn().Str("uploadID", uploadID).Dur("maxAge", maxAge).Msg("removing stale upload")
		uc.removeUploadFiles(uploadID)
		if err := uc.uploadRepo.UpdateStatus(context.Background(), uploadID, entity.UploadStatusFailed); err != nil {
			log.Error().Err(err).Str("uploadID", uploadID).Msg("stale upload cleanup: failed to update status")
		}
	}
}

// uploadDirFor returns the directory an upload's file should live in:
// {uploadDir}/{versionName}/{species}, except orthology.tsv (species == "")
// which keeps the flat {uploadDir}/{versionName} path since it is shared
// across every assembly in the Database Version rather than owned by one.
func (uc *UseCase) uploadDirFor(versionName, species string) string {
	if species == "" {
		return filepath.Join(uc.uploadDir, versionName)
	}
	return filepath.Join(uc.uploadDir, versionName, species)
}

// parseOptionalAssemblyVersionID reads "_assemblyVersionID" from upload
// metadata, returning nil if absent (the orthology.tsv case, which has no
// owning assembly).
func parseOptionalAssemblyVersionID(meta tusd.MetaData) (*uint64, error) {
	raw, ok := meta["_assemblyVersionID"]
	if !ok || raw == "" {
		return nil, nil
	}
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse _assemblyVersionID: %w", err)
	}
	return &id, nil
}

func parseCommaSeparated(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			result = append(result, v)
		}
	}
	return result
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
