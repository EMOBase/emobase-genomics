package api

import (
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
	store := filestore.New("./data/public/uploads")

	// A locking mechanism helps preventing data loss or corruption from
	// parallel requests to a upload resource. A good match for the disk-based
	// storage is the filelocker package which uses disk-based file lock for
	// coordinating access.
	// More information is available at https://tus.github.io/tusd/advanced-topics/locks/.
	locker := filelocker.New("./data/public/uploads")

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
		BasePath:              "/api/uploads",
		StoreComposer:         composer,
		NotifyCreatedUploads:  true,
		NotifyCompleteUploads: true,
	})
	if err != nil {
		log.Error().Err(err).Msg("unable to create tusd handler")
		return nil, err
	}

	return handler, nil
}
