package handlers

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	"github.com/EMOBase/emobase-genomics/internal/pkg/jobpayload"
	"github.com/EMOBase/emobase-genomics/internal/pkg/uploadspec"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

type IBundleUploadFileRepository interface {
	FindByID(ctx context.Context, id string) (*entity.UploadFile, error)
	Create(ctx context.Context, f *entity.UploadFile) error
	UpdateStatus(ctx context.Context, id string, status entity.UploadStatus) error
}

type IBundleJobRepository interface {
	Create(ctx context.Context, j *entity.Job) error
	FindLatestByFileAndType(ctx context.Context, fileID string, jobType string) (*entity.Job, error)
}

// BundleHandler extracts a bundle archive (.tar.gz holding data files plus a
// manifest.csv) into independent child upload files, and enqueues one child
// job per file — exactly as if each file had been uploaded on its own.
//
// It is safe to rerun after a crash (stuck-job recovery): child IDs are derived
// from the bundle ID and file name, and existing child rows/jobs are reused.
type BundleHandler struct {
	spec           uploadspec.BundleSpec
	uploadFileRepo IBundleUploadFileRepository
	jobRepo        IBundleJobRepository
	versionRepo    IVersionRepository
}

func NewBundleHandler(
	spec uploadspec.BundleSpec,
	uploadFileRepo IBundleUploadFileRepository,
	jobRepo IBundleJobRepository,
	versionRepo IVersionRepository,
) *BundleHandler {
	return &BundleHandler{
		spec:           spec,
		uploadFileRepo: uploadFileRepo,
		jobRepo:        jobRepo,
		versionRepo:    versionRepo,
	}
}

type bundleResult struct {
	FileIDs []string `json:"fileIds"`
	JobIDs  []uint64 `json:"jobIds"`
}

func (h *BundleHandler) Handle(ctx context.Context, job entity.Job) (json.RawMessage, error) {
	var payload jobpayload.ProcessPayload
	if err := json.Unmarshal(*job.Payload, &payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal job payload: %w", err)
	}

	version, err := h.versionRepo.FindByID(ctx, payload.VersionID)
	if err != nil {
		return nil, fmt.Errorf("failed to look up version: %w", err)
	}
	if version == nil {
		return nil, fmt.Errorf("version %d not found", payload.VersionID)
	}

	bundle, err := h.uploadFileRepo.FindByID(ctx, payload.UploadFileID)
	if err != nil {
		return nil, fmt.Errorf("failed to look up bundle upload file: %w", err)
	}
	if bundle == nil {
		return nil, fmt.Errorf("bundle upload file %q not found", payload.UploadFileID)
	}

	// Pass 1: validate archive layout and manifest before writing anything,
	// so a bad bundle never leaves files or rows behind.
	entries, manifest, err := scanBundle(payload.FilePath)
	if err != nil {
		return nil, err
	}
	rows, err := parseManifest(manifest, h.spec, entries)
	if err != nil {
		return nil, err
	}

	// Pass 2: write each file gzipped into a per-bundle directory, so names
	// never collide with other uploads of the same version.
	dstDir := filepath.Join(filepath.Dir(payload.FilePath), payload.UploadFileID)
	if err := extractBundle(payload.FilePath, dstDir); err != nil {
		h.removeDirIfNoChildren(ctx, dstDir, payload.UploadFileID, rows)
		return nil, err
	}

	// Children inherit the bundle's assembly: nil for version-scoped bundles
	// (orthology), the one assembly the bundle covers otherwise.
	assemblyVersionID := bundle.AssemblyVersionID
	var result bundleResult
	for _, meta := range rows {
		fileName := meta["fileName"]
		childID := bundleChildID(payload.UploadFileID, fileName)
		filePath := filepath.Join(dstDir, storedName(fileName))

		if err := h.ensureChildFile(ctx, childID, filePath, version.ID, assemblyVersionID, bundle.CreatedBy); err != nil {
			return nil, err
		}
		jobID, err := h.ensureChildJob(ctx, childID, filePath, version, assemblyVersionID, meta)
		if err != nil {
			return nil, err
		}
		result.FileIDs = append(result.FileIDs, childID)
		result.JobIDs = append(result.JobIDs, jobID)
	}

	log.Ctx(ctx).Info().
		Uint64("jobID", job.ID).
		Str("bundleID", payload.UploadFileID).
		Int("files", len(rows)).
		Msgf("%s bundle extracted", h.spec.ChildFileType)

	return json.Marshal(result)
}

// OnComplete removes the archive once the job is DONE. Doing it here rather
// than in Handle keeps the archive available if the worker dies before the
// job is marked DONE, so the requeued job can still rerun.
func (h *BundleHandler) OnComplete(ctx context.Context, job entity.Job, _ json.RawMessage) error {
	var payload jobpayload.ProcessPayload
	if err := json.Unmarshal(*job.Payload, &payload); err != nil {
		return fmt.Errorf("failed to unmarshal job payload: %w", err)
	}
	if err := os.Remove(payload.FilePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove bundle archive: %w", err)
	}
	return nil
}

func (h *BundleHandler) ensureChildFile(ctx context.Context, childID, filePath string, versionID uint64, assemblyVersionID *uint64, createdBy string) error {
	existing, err := h.uploadFileRepo.FindByID(ctx, childID)
	if err != nil {
		return fmt.Errorf("failed to look up child upload file %q: %w", childID, err)
	}
	if existing == nil {
		info, err := os.Stat(filePath)
		if err != nil {
			return fmt.Errorf("failed to stat extracted file: %w", err)
		}
		if err := h.uploadFileRepo.Create(ctx, &entity.UploadFile{
			ID:                childID,
			VersionID:         versionID,
			AssemblyVersionID: assemblyVersionID,
			FilePath:          filePath,
			FileType:          h.spec.ChildFileType,
			FileSize:          info.Size(),
			UploadStatus:      entity.UploadStatusUploading,
			CreatedBy:         createdBy,
		}); err != nil {
			return fmt.Errorf("failed to create child upload file %q: %w", childID, err)
		}
	}
	if existing == nil || existing.UploadStatus != entity.UploadStatusCompleted {
		// UpdateStatus also sets completed_at, which Create does not.
		if err := h.uploadFileRepo.UpdateStatus(ctx, childID, entity.UploadStatusCompleted); err != nil {
			return fmt.Errorf("failed to mark child upload file %q completed: %w", childID, err)
		}
	}
	return nil
}

func (h *BundleHandler) ensureChildJob(ctx context.Context, childID, filePath string, version *entity.Version, assemblyVersionID *uint64, meta map[string]string) (uint64, error) {
	existing, err := h.jobRepo.FindLatestByFileAndType(ctx, childID, h.spec.ChildJobType)
	if err != nil {
		return 0, fmt.Errorf("failed to look up %s job for %q: %w", h.spec.ChildJobType, childID, err)
	}
	if existing != nil {
		return existing.ID, nil
	}
	job, err := h.spec.NewJob(version.ID, assemblyVersionID, childID, filePath, meta)
	if err != nil {
		return 0, err
	}
	if err := h.jobRepo.Create(ctx, job); err != nil {
		return 0, fmt.Errorf("failed to create %s job for %q: %w", h.spec.ChildJobType, childID, err)
	}
	return job.ID, nil
}

// removeDirIfNoChildren cleans up a failed extraction, unless an earlier run of
// this job already created children whose jobs may be reading those files.
func (h *BundleHandler) removeDirIfNoChildren(ctx context.Context, dstDir, bundleID string, rows []map[string]string) {
	for _, meta := range rows {
		f, err := h.uploadFileRepo.FindByID(ctx, bundleChildID(bundleID, meta["fileName"]))
		if err != nil || f != nil {
			return
		}
	}
	if err := os.RemoveAll(dstDir); err != nil {
		log.Ctx(ctx).Warn().Err(err).Str("dir", dstDir).Msg("failed to remove partially extracted bundle")
	}
}

// bundleChildID is deterministic so a rerun finds the children it already created.
func bundleChildID(bundleID, fileName string) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("emobase-bundle:"+bundleID+"/"+fileName)).String()
}

// storedName is the on-disk name of an extracted file. Child handlers read
// their input through gzip.NewReader, and some (e.g. add_jbrowse_track.sh)
// strip a .gz/.gzip suffix to recover the original extension.
func storedName(name string) string {
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, ".gz") || strings.HasSuffix(lower, ".gzip") {
		return name
	}
	return name + ".gz"
}

// bundleEntryName returns the validated name of a tar entry, or "" for entries
// that are skipped (the archive root directory, pax global headers). Only flat
// regular files are accepted, so extraction can never write outside dstDir.
func bundleEntryName(hdr *tar.Header) (string, error) {
	name := path.Clean(hdr.Name)
	switch hdr.Typeflag {
	case tar.TypeXGlobalHeader:
		return "", nil
	case tar.TypeDir:
		if name == "." { // "./" from `tar czf bundle.tar.gz -C <folder> .`
			return "", nil
		}
		return "", fmt.Errorf("%q: folders are not supported; put files at the archive root, e.g. `tar czf bundle.tar.gz -C <folder> .`", hdr.Name)
	case tar.TypeReg:
	default:
		return "", fmt.Errorf("%q: only regular files are supported (no links or special files)", hdr.Name)
	}
	if strings.Contains(name, "/") || name == ".." || name == "." {
		return "", fmt.Errorf("%q: files must be at the archive root, not inside a folder", hdr.Name)
	}
	if strings.HasPrefix(name, "._") {
		return "", fmt.Errorf("%q: macOS metadata file; create the archive with `COPYFILE_DISABLE=1 tar czf ...`", hdr.Name)
	}
	return name, nil
}

func openBundle(archivePath string) (*os.File, *gzip.Reader, *tar.Reader, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to open bundle archive %q: %w", archivePath, err)
	}
	gr, err := gzip.NewReader(f)
	if err != nil {
		_ = f.Close()
		return nil, nil, nil, fmt.Errorf("failed to create gzip reader: %w", err)
	}
	return f, gr, tar.NewReader(gr), nil
}

// scanBundle reads the whole archive without writing anything. It returns the
// set of data file names and the manifest contents, and reports every invalid
// entry at once.
func scanBundle(archivePath string) (map[string]bool, []byte, error) {
	f, gr, tr, err := openBundle(archivePath)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = f.Close() }()
	defer func() { _ = gr.Close() }()

	entries := map[string]bool{}
	seen := map[string]bool{}
	stored := map[string]string{} // stored name → entry name
	var manifest []byte
	var errs []error
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read bundle archive: %w", err)
		}
		name, err := bundleEntryName(hdr)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if name == "" {
			continue
		}
		if seen[name] {
			errs = append(errs, fmt.Errorf("%q appears more than once in the archive", name))
			continue
		}
		seen[name] = true

		if name == uploadspec.ManifestFileName {
			if manifest, err = io.ReadAll(tr); err != nil {
				return nil, nil, fmt.Errorf("failed to read %s: %w", uploadspec.ManifestFileName, err)
			}
			continue
		}
		s := storedName(name)
		if other, ok := stored[s]; ok {
			errs = append(errs, fmt.Errorf("%q and %q would both be stored as %q; rename one", other, name, s))
			continue
		}
		stored[s] = name
		entries[name] = true
	}
	// Read to the gzip trailer so a truncated or corrupt archive fails its
	// checksum here, before anything is written.
	if _, err := io.Copy(io.Discard, gr); err != nil {
		return nil, nil, fmt.Errorf("failed to read bundle archive: %w", err)
	}
	if manifest == nil {
		errs = append(errs, fmt.Errorf("archive has no %s at its root", uploadspec.ManifestFileName))
	}
	if len(errs) > 0 {
		return nil, nil, errors.Join(errs...)
	}
	return entries, manifest, nil
}

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// parseManifest validates manifest.csv against the bundle spec and the archive
// contents, returning one metadata map per file. Every problem is reported at
// once so the user can fix the manifest in a single round trip.
func parseManifest(data []byte, spec uploadspec.BundleSpec, entries map[string]bool) ([]map[string]string, error) {
	mf := uploadspec.ManifestFileName
	// Spreadsheet apps (e.g. Excel "CSV UTF-8") prepend a BOM to the first header cell.
	records, err := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(data, utf8BOM))).ReadAll()
	if err != nil {
		return nil, fmt.Errorf("invalid %s: %w", mf, err)
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("%s is empty", mf)
	}

	header := make([]string, len(records[0]))
	for i, col := range records[0] {
		header[i] = strings.TrimSpace(col)
	}
	if err := checkManifestHeader(header, spec.Columns); err != nil {
		return nil, fmt.Errorf("invalid %s header: %w", mf, err)
	}

	var rows []map[string]string
	var errs []error
	listed := map[string]bool{}
	uniqueValues := map[string]string{} // UniqueColumn value → fileName
	for i, rec := range records[1:] {
		meta := make(map[string]string, len(header))
		blank := true
		for j, col := range header {
			meta[col] = strings.TrimSpace(rec[j])
			if meta[col] != "" {
				blank = false
			}
		}
		if blank { // spreadsheet exports often end with empty rows
			continue
		}

		fileName := meta["fileName"]
		rowErr := func(err error) {
			errs = append(errs, fmt.Errorf("%s line %d (%s): %w", mf, i+2, fileName, err))
		}
		switch {
		case fileName == "":
			rowErr(errors.New("fileName is required"))
			continue
		case listed[fileName]:
			rowErr(errors.New("fileName is listed more than once"))
			continue
		case !entries[fileName]:
			rowErr(errors.New("file not found in the archive"))
			continue
		}
		listed[fileName] = true

		if err := spec.Validate(meta); err != nil {
			rowErr(err)
		}
		if spec.UniqueColumn != "" && meta[spec.UniqueColumn] != "" {
			v := meta[spec.UniqueColumn]
			if other, dup := uniqueValues[v]; dup {
				rowErr(fmt.Errorf("%s %q is already used by %s", spec.UniqueColumn, v, other))
			} else {
				uniqueValues[v] = fileName
			}
		}
		rows = append(rows, meta)
	}

	for _, name := range slices.Sorted(maps.Keys(entries)) {
		if !listed[name] {
			errs = append(errs, fmt.Errorf("%q is in the archive but not listed in %s", name, mf))
		}
	}
	if len(rows) == 0 && len(errs) == 0 {
		errs = append(errs, fmt.Errorf("%s lists no files", mf))
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return rows, nil
}

// checkManifestHeader requires exactly the template's columns, in any order.
func checkManifestHeader(header, columns []string) error {
	want := map[string]bool{}
	for _, c := range columns {
		want[c] = true
	}
	var errs []error
	got := map[string]bool{}
	for _, c := range header {
		switch {
		case got[c]:
			errs = append(errs, fmt.Errorf("duplicate column %q", c))
		case !want[c]:
			errs = append(errs, fmt.Errorf("unknown column %q", c))
		}
		got[c] = true
	}
	for _, c := range columns {
		if !got[c] {
			errs = append(errs, fmt.Errorf("missing column %q", c))
		}
	}
	return errors.Join(errs...)
}

// extractBundle writes every data file of an already-validated archive into
// dstDir, gzipping plain files and copying already-gzipped ones unchanged.
// Each file is written to a temp name and renamed into place, so a rerun never
// truncates a file that a child job may already be reading.
func extractBundle(archivePath, dstDir string) error {
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return fmt.Errorf("failed to create bundle directory: %w", err)
	}

	f, gr, tr, err := openBundle(archivePath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	defer func() { _ = gr.Close() }()

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("failed to read bundle archive: %w", err)
		}
		name, err := bundleEntryName(hdr)
		if err != nil { // already rejected by scanBundle; checked again for safety
			return err
		}
		if name == "" || name == uploadspec.ManifestFileName {
			continue
		}
		if err := writeGzipped(tr, filepath.Join(dstDir, storedName(name))); err != nil {
			return fmt.Errorf("failed to extract %q: %w", name, err)
		}
	}
}

func writeGzipped(r io.Reader, dst string) error {
	br := bufio.NewReader(r)
	magic, _ := br.Peek(2) // shorter files are simply not gzip
	alreadyGzip := len(magic) == 2 && magic[0] == 0x1f && magic[1] == 0x8b

	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if alreadyGzip {
		_, err = io.Copy(out, br)
	} else {
		gw := gzip.NewWriter(out)
		if _, err = io.Copy(gw, br); err == nil {
			err = gw.Close()
		}
	}
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}
