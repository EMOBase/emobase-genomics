package entity

// JobStatusCounts holds aggregated job status counts for a single version.
type JobStatusCounts struct {
	RunningCount int
	FailedCount  int
	DoneCount    int
	TotalCount   int
}
