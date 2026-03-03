# EMOBase Genomics

## Requirements

- Go v1.25.0.
- Local Elasticsearch instance.
- Data files placed in `cmd/data` directory.
  - Currently supported files:
    - `genomic.gff`
    - `rna.fna`
    - `cds.fna`
    - `protein.faa`
    - `*_orthology.tsv`

## How to Run

1. Set the `ES_PASSWORD` environment variable.
2. Run the application:

   ```bash
   go run ./cmd
   ```

The application will:

- Parse the data files located in `cmd/data`.

- Insert records into the following Elasticsearch indexes: `genomiclocation`, `sequence`, `orthology`, `synonym`.

## Known issues
