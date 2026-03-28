package api

import (
	"os"
	"path/filepath"

	"github.com/rs/zerolog/log"
	"github.com/tus/tusd/v2/pkg/filelocker"
	"github.com/tus/tusd/v2/pkg/filestore"
	tusd "github.com/tus/tusd/v2/pkg/handler"
)

func NewTUSHandler() (*tusd.Handler, error) {
	// Create a new FileStore instance which is responsible for
	// storing the uploaded file on disk in the specified directory.
	// This path _must_ exist before tusd will store uploads in it.
	// If you want to save them on a different medium, for example
	// a remote FTP server, you can implement your own storage backend
	// by implementing the tusd.DataStore interface.
	store := filestore.New("./public/uploads")

	// A locking mechanism helps preventing data loss or corruption from
	// parallel requests to a upload resource. A good match for the disk-based
	// storage is the filelocker package which uses disk-based file lock for
	// coordinating access.
	// More information is available at https://tus.github.io/tusd/advanced-topics/locks/.
	locker := filelocker.New("./public/uploads")

	// A storage backend for tusd may consist of multiple different parts which
	// handle upload creation, locking, termination and so on. The composer is a
	// place where all those separated pieces are joined together. In this example
	// we only use the file store but you may plug in multiple.
	composer := tusd.NewStoreComposer()
	store.UseIn(composer)
	locker.UseIn(composer)

	// Create a new HTTP handler for the tusd server by providing a configuration.
	// The StoreComposer property must be set to allow the handler to function.
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

	// Start another goroutine for receiving events from the handler whenever
	// an upload is completed. The event will contains details about the upload
	// itself and the relevant HTTP request.
	go func() {
		for {
			select {
			case event := <-handler.CreatedUploads:
				log.Info().
					Any("metaData", event.Upload.MetaData).
					Msg("upload created")
			case event := <-handler.CompleteUploads:
				upload := event.Upload

				version, ok := upload.MetaData["version"]
				if !ok || version == "" {
					log.Warn().
						Str("uploadID", upload.ID).
						Msg("no version in metadata, skipping rename")
					continue
				}

				filename, ok := upload.MetaData["filename"]
				if !ok || filename == "" {
					log.Warn().
						Str("uploadID", upload.ID).
						Msg("no filename in metadata, skipping rename")
					continue
				}

				// Sanitize both fields to prevent path traversal
				version = filepath.Base(version)
				filename = filepath.Base(filename)

				dstDir := filepath.Join("./public/uploads", version)
				srcPath := filepath.Join("./public/uploads", upload.ID)
				dstPath := filepath.Join(dstDir, filename)

				// Create the version directory if it doesn't exist
				if err := os.MkdirAll(dstDir, 0755); err != nil {
					log.Error().Err(err).Str("dir", dstDir).Msg("failed to create version directory")
					continue
				}

				if err := os.Rename(srcPath, dstPath); err != nil {
					log.Error().Err(err).Str("src", srcPath).Str("dst", dstPath).Msg("failed to rename upload")
					continue
				}

				// Clean up the .info sidecar file
				infoPath := filepath.Join("./public/uploads", upload.ID+".info")
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
	}()

	return handler, nil
}
