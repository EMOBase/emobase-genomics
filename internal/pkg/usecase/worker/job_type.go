package worker

// JobDescriptions maps each job type to a human-readable title shown in the UI.
var JobDescriptions = map[string]string{
	JobTypeGenomicGFF:              "Parse genomic GFF file",
	JobTypeRNAFNA:                  "Parse RNA FASTA file",
	JobTypeCDSFNA:                  "Parse CDS FASTA file",
	JobTypeProteinFAA:              "Parse protein FASTA file",
	JobTypeOrthologyTSV:            "Parse orthology TSV file",
	JobTypeOrthologyTSVDelete:      "Delete orthology TSV file",
	JobTypeGenomicGFFSynonym:       "Build gene synonyms",
	JobTypeGenomicFNASetupBlast:    "Setup genome BLAST database",
	JobTypeProteinFAASetupBlast:    "Setup protein BLAST database",
	JobTypeRNAFNASetupBlast:        "Setup RNA BLAST database",
	JobTypeGenomicFNASetupJBrowse2: "Setup JBrowse2 genome browser",
}

// Job type constants — must match the values stored in jobs.type.
const (
	JobTypeGenomicGFF   = "GENOMIC.GFF"
	JobTypeRNAFNA       = "RNA.FNA"
	JobTypeCDSFNA       = "CDS.FNA"
	JobTypeProteinFAA   = "PROTEIN.FAA"
	JobTypeOrthologyTSV       = "ORTHOLOGY.TSV"
	JobTypeOrthologyTSVDelete = "ORTHOLOGY.TSV:DELETE"

	JobTypeGenomicGFFSynonym = "GENOMIC.GFF:SYNONYM"

	// SETUP_BLAST jobs run makeblastdb to build a SequenceServer-compatible
	// BLAST database from the processed file.
	JobTypeGenomicFNASetupBlast = "GENOMIC.FNA:SETUP_BLAST"
	JobTypeProteinFAASetupBlast = "PROTEIN.FAA:SETUP_BLAST"
	JobTypeRNAFNASetupBlast     = "RNA.FNA:SETUP_BLAST"

	// SETUP_JBROWSE2 builds the JBrowse2 genome browser tracks.
	// Requires both GENOMIC.GFF and GENOMIC.FNA:SETUP_BLAST to be done first.
	JobTypeGenomicFNASetupJBrowse2 = "GENOMIC.FNA:SETUP_JBROWSE2"
)
