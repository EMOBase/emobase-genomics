package upload

import (
	"os"
	"path/filepath"

	"github.com/rs/zerolog/log"
	"github.com/tus/tusd/v2/pkg/filelocker"
	"github.com/tus/tusd/v2/pkg/filestore"
	tusd "github.com/tus/tusd/v2/pkg/handler"
)

type UseCase struct {
	Handler *tusd.Handler
}

func New(uploadDir string) (*UseCase, error) {
	store := filestore.New(uploadDir)
	locker := filelocker.New(uploadDir)

	composer := tusd.NewStoreComposer()
	store.UseIn(composer)
	locker.UseIn(composer)

	handler, err := tusd.NewHandler(tusd.Config{
		BasePath:              "/uploads",
		StoreComposer:         composer,
		DisableDownload:       true,
		NotifyCreatedUploads:  true,
		NotifyCompleteUploads: true,
	})
	if err != nil {
		log.Error().Err(err).Msg("unable to create tusd handler")
		return nil, err
	}

	uc := &UseCase{Handler: handler}
	go uc.processEvents(uploadDir)

	return uc, nil
}

func (uc *UseCase) processEvents(uploadDir string) {
	for {
		select {
		case event := <-uc.Handler.CreatedUploads:
			log.Info().
				Any("metaData", event.Upload.MetaData).
				Msg("upload created")

		case event := <-uc.Handler.CompleteUploads:
			upload := event.Upload

			version, ok := upload.MetaData["version"]
			if !ok || version == "" {
				log.Warn().Str("uploadID", upload.ID).Msg("no version in metadata, skipping rename")
				continue
			}

			filename, ok := upload.MetaData["filename"]
			if !ok || filename == "" {
				log.Warn().Str("uploadID", upload.ID).Msg("no filename in metadata, skipping rename")
				continue
			}

			// Sanitize both fields to prevent path traversal
			version = filepath.Base(version)
			filename = filepath.Base(filename)

			dstDir := filepath.Join(uploadDir, version)
			srcPath := filepath.Join(uploadDir, upload.ID)
			dstPath := filepath.Join(dstDir, filename)

			if err := os.MkdirAll(dstDir, 0755); err != nil {
				log.Error().Err(err).Str("dir", dstDir).Msg("failed to create version directory")
				continue
			}

			if err := os.Rename(srcPath, dstPath); err != nil {
				log.Error().Err(err).Str("src", srcPath).Str("dst", dstPath).Msg("failed to rename upload")
				continue
			}

			infoPath := filepath.Join(uploadDir, upload.ID+".info")
			if err := os.Remove(infoPath); err != nil {
				log.Warn().Err(err).Msg("failed to remove .info file")
			}

			log.Info().
				Str("uploadID", upload.ID).
				Str("version", version).
				Str("filename", filename).
				Str("path", dstPath).
				Msg("upload complete, file moved")
		}
	}
}
