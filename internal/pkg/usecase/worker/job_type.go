package worker

// Job type constants — must match the values stored in jobs.type.
const (
	JobTypeGenomicGFF   = "GENOMIC.GFF"
	JobTypeRNAFNA       = "RNA.FNA"
	JobTypeCDSFNA       = "CDS.FNA"
	JobTypeProteinFAA   = "PROTEIN.FAA"
	JobTypeOrthologyTSV = "ORTHOLOGY.TSV"

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
