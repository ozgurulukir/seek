package indexer

// SyncReport is the machine-readable outcome of one collection sync. The
// human progress log remains useful, but callers can now distinguish source
// skips, unsupported formats, persistence failures, and successfully indexed
// documents without parsing text output.
type SyncReport struct {
	Collection  string        `json:"collection"`
	Indexed     int           `json:"indexed"`
	Skipped     int           `json:"skipped"`
	Unsupported int           `json:"unsupported"`
	Failed      int           `json:"failed"`
	Warnings    int           `json:"warnings"`
	Errors      []SyncFailure `json:"errors,omitempty"`
}

type SyncFailure struct {
	Path  string `json:"path,omitempty"`
	Kind  string `json:"kind"`
	Error string `json:"error"`
}

func (r *SyncReport) add(other SyncReport) {
	r.Indexed += other.Indexed
	r.Skipped += other.Skipped
	r.Unsupported += other.Unsupported
	r.Failed += other.Failed
	r.Warnings += other.Warnings
	r.Errors = append(r.Errors, other.Errors...)
}
