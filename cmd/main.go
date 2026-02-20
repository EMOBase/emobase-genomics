package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	genomicrepo "github.com/EMOBase/emobase-genomics/internal/pkg/repository/genomic"
	genomicusecase "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/genomic"
	"github.com/elastic/go-elasticsearch/v8"
)

func main() {
	fmt.Println("Hello, EMOBase Genomics!")
	startTime := time.Now()

	ctx := context.Background()

	genomicFile, err := os.Open("./cmd/data/genomic.gff")
	if err != nil {
		panic(err)
	}
	defer genomicFile.Close()

	// Init repositories
	esClient, err := elasticsearch.NewClient(elasticsearch.Config{
		Addresses: []string{"http://localhost:9200"},
		Username:  "elastic",
		Password:  os.Getenv("ES_PASSWORD"),
	})
	if err != nil {
		log.Fatal(err)
	}

	// TODO: Remove data in the index before loading new data (development purpose only)

	genomicLocationRepository := genomicrepo.NewElasticSearchRepository(esClient, "genomiclocations")

	// Init usecases
	genomicUsecase := genomicusecase.NewGenomicLocationUseCase(genomicLocationRepository)

	err = genomicUsecase.Load(ctx, genomicFile)
	if err != nil {
		panic(err)
	}

	fmt.Println("Genomic data loaded successfully.")
	fmt.Println("Elapsed time:", time.Since(startTime))
}
