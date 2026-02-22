package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	genomicrepo "github.com/EMOBase/emobase-genomics/internal/pkg/repository/genomic"
	sequencerepo "github.com/EMOBase/emobase-genomics/internal/pkg/repository/sequence"
	genomicusecase "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/genomic"
	sequenceusecase "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/sequence"
	"github.com/elastic/go-elasticsearch/v8"
)

func main() {
	fmt.Println("Hello, EMOBase Genomics!")

	ctx := context.Background()

	genomicFile, err := os.Open("./cmd/data/genomic.gff")
	if err != nil {
		panic(err)
	}
	defer genomicFile.Close()

	cdsFile, err := os.Open("./cmd/data/cds.fna")
	if err != nil {
		panic(err)
	}
	defer cdsFile.Close()

	proteinFile, err := os.Open("./cmd/data/protein.faa")
	if err != nil {
		panic(err)
	}

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

	genomicLocationRepository := genomicrepo.NewElasticSearchRepository(esClient, "genomiclocation")
	sequenceRepository := sequencerepo.NewElasticSearchRepository(esClient, "sequence")

	// Init usecases
	genomicUsecase := genomicusecase.NewGenomicLocationUseCase(genomicLocationRepository)
	sequenceUsecase := sequenceusecase.NewSequenceUseCase(sequenceRepository)

	startTime := time.Now()
	err = genomicUsecase.Load(ctx, genomicFile)
	if err != nil {
		panic(err)
	}

	fmt.Println("Genomic data loaded successfully.")
	fmt.Println("Elapsed time:", time.Since(startTime))

	startTime = time.Now()
	err = sequenceUsecase.Load(ctx, cdsFile)
	if err != nil {
		panic(err)
	}

	fmt.Println("CDS Sequence data loaded successfully.")
	fmt.Println("Elapsed time:", time.Since(startTime))

	startTime = time.Now()
	err = sequenceUsecase.Load(ctx, proteinFile)
	if err != nil {
		panic(err)
	}

	fmt.Println("Protein Sequence data loaded successfully.")
	fmt.Println("Elapsed time:", time.Since(startTime))
}
