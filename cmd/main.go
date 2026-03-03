package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	genomicrepo "github.com/EMOBase/emobase-genomics/internal/pkg/repository/genomic"
	orthologyrepo "github.com/EMOBase/emobase-genomics/internal/pkg/repository/orthology"
	sequencerepo "github.com/EMOBase/emobase-genomics/internal/pkg/repository/sequence"
	synonymrepo "github.com/EMOBase/emobase-genomics/internal/pkg/repository/synonym"
	genomicusecase "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/genomic"
	orthologyusecase "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/orthology"
	sequenceusecase "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/sequence"
	synonymusecase "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/synonym"
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

	orthologyFile, err := os.Open("./cmd/data/1.OrthoDB_orthology.tsv")
	if err != nil {
		panic(err)
	}

	// Init repositories
	esPort := os.Getenv("ES_PORT")
	if esPort == "" {
		esPort = "9200"
	}

	esClient, err := elasticsearch.NewClient(elasticsearch.Config{
		Addresses: []string{"http://localhost:" + esPort},
		Username:  "elastic",
		Password:  os.Getenv("ES_PASSWORD"),
	})
	if err != nil {
		log.Fatal(err)
	}

	// TODO: Put this into a separate command?
	err = esMigrate(ctx, esClient)
	if err != nil {
		log.Fatal(err)
	}

	// TODO: Remove data in the index before loading new data (development purpose only)

	genomicLocationRepository := genomicrepo.NewElasticSearchRepository(esClient, "genomiclocation")
	sequenceRepository := sequencerepo.NewElasticSearchRepository(esClient, "sequence")
	orthologyRepository := orthologyrepo.NewElasticSearchRepository(esClient, "orthology")
	synonymRepository := synonymrepo.NewElasticSearchRepository(esClient, "synonym")

	// Init usecases
	genomicUsecase := genomicusecase.NewGenomicLocationUseCase(genomicLocationRepository)
	sequenceUsecase := sequenceusecase.NewSequenceUseCase(sequenceRepository)
	orthologyUsecase := orthologyusecase.NewOrthologyUseCase(orthologyRepository)
	synonymUsecase := synonymusecase.NewSynonymUseCase(synonymRepository)

	startTime := time.Now()
	err = genomicUsecase.Load(ctx, genomicFile)
	if err != nil {
		panic(err)
	}

	fmt.Println("Genomic data loaded successfully.")
	fmt.Println("Elapsed time:", time.Since(startTime))
	fmt.Println()

	startTime = time.Now()
	err = sequenceUsecase.Load(ctx, cdsFile)
	if err != nil {
		panic(err)
	}

	fmt.Println("CDS Sequence data loaded successfully.")
	fmt.Println("Elapsed time:", time.Since(startTime))
	fmt.Println()

	startTime = time.Now()
	err = sequenceUsecase.Load(ctx, proteinFile)
	if err != nil {
		panic(err)
	}

	fmt.Println("Protein Sequence data loaded successfully.")
	fmt.Println("Elapsed time:", time.Since(startTime))
	fmt.Println()

	startTime = time.Now()
	err = orthologyUsecase.Load(ctx, orthologyFile)
	if err != nil {
		panic(err)
	}

	fmt.Println("Orthology data loaded successfully.")
	fmt.Println("Elapsed time:", time.Since(startTime))
	fmt.Println()

	startTime = time.Now()
	_, err = genomicFile.Seek(0, io.SeekStart)
	if err != nil {
		panic(err)
	}

	err = synonymUsecase.Load(ctx, genomicFile)
	if err != nil {
		panic(err)
	}

	fmt.Println("Synonym data loaded successfully.")
	fmt.Println("Elapsed time:", time.Since(startTime))
	fmt.Println()
}
