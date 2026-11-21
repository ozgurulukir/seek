package app

import (
	"testing"

	"github.com/ozgurulukir/seek/internal/config"
)

// T1 from the 2026-11-21 tech-debt audit: the bucket-limit clamping in
// NewStoreSearchRepositoryWithLimit is a one-line contract (<=0 falls back
// to the config-owned default) with real operator meaning — 0/ Negative
// limits come from misconfigured YAML — so pin it with a table test.
func TestNewStoreSearchRepositoryWithLimitClampsNonPositive(t *testing.T) {
	tests := []struct {
		name  string
		limit int
		want  int
	}{
		{"zero falls back to config default", 0, config.DefaultAggregationBucketLimit},
		{"negative falls back to config default", -1, config.DefaultAggregationBucketLimit},
		{"negative far below default clamps", -100, config.DefaultAggregationBucketLimit},
		{"positive passes through", 7, 7},
		{"large positive passes through", 1 << 20, 1 << 20},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewStoreSearchRepositoryWithLimit(nil, tt.limit)
			if r == nil {
				t.Fatal("constructor returned nil repository")
			}
			if r.defaultBucketLimit != tt.want {
				t.Errorf("defaultBucketLimit = %d, want %d", r.defaultBucketLimit, tt.want)
			}
		})
	}
}
