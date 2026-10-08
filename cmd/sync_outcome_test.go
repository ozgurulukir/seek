package cmd

import (
	"errors"
	"fmt"
	"testing"

	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/indexer"
	"github.com/ozgurulukir/seek/internal/source"
	"github.com/ozgurulukir/seek/internal/source/parserdef"
)

func TestSyncOutcomeExitPolicy(t *testing.T) {
	// Capability is config-only: a configured key is what makes embeddings
	// available, independent of any network client.
	capable := &config.AppConfig{Config: config.Config{Embedding: config.EmbeddingConfig{APIKey: "test-key"}}}
	noKey := &config.AppConfig{}
	for _, tc := range []struct {
		name       string
		cfg        *config.AppConfig
		cmd        SyncCmd
		report     indexer.SyncReport
		err        error
		status     string
		embeddings string
		failed     bool
	}{
		{name: "missing Claude bulk", err: fmt.Errorf("discovery: %w", source.ErrUnavailable), status: "skipped", embeddings: "not_run"},
		{name: "missing parser bulk", err: fmt.Errorf("discovery: %w", parserdef.ErrUnavailable), status: "skipped", embeddings: "not_run"},
		{name: "explicit source", cmd: SyncCmd{Collection: "claude"}, err: source.ErrUnavailable, status: "skipped", embeddings: "not_run", failed: true},
		{name: "strict source", cmd: SyncCmd{Strict: true}, err: source.ErrUnavailable, status: "skipped", embeddings: "not_run", failed: true},
		{name: "hard failure", err: errors.New("permission denied"), status: "failed", embeddings: "not_run", failed: true},
		{name: "individual failure", report: indexer.SyncReport{Indexed: 1, Failed: 1}, status: "failed", embeddings: "unavailable", failed: true},
		{name: "individual failure with key", cfg: capable, report: indexer.SyncReport{Indexed: 1, Failed: 1}, status: "failed", embeddings: "completed", failed: true},
		{name: "individual failure keyword only", cmd: SyncCmd{NoEmbed: true}, report: indexer.SyncReport{Indexed: 1, Failed: 1}, status: "failed", embeddings: "skipped_requested", failed: true},
		{name: "strict failure keeps embedding reason", cfg: noKey, cmd: SyncCmd{Strict: true}, report: indexer.SyncReport{Indexed: 1, Failed: 1}, status: "failed", embeddings: "unavailable", failed: true},
		{name: "keyword success", report: indexer.SyncReport{Indexed: 1}, status: "degraded", embeddings: "unavailable"},
		{name: "strict embedding", cmd: SyncCmd{Strict: true}, status: "degraded", embeddings: "unavailable", failed: true},
		{name: "requested keyword only", cfg: capable, cmd: SyncCmd{Strict: true, NoEmbed: true}, status: "success", embeddings: "skipped_requested"},
		{name: "embeddings completed", cfg: capable, report: indexer.SyncReport{Indexed: 1}, status: "success", embeddings: "completed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := tc.cfg
			if cfg == nil {
				cfg = noKey
			}
			out, failed := tc.cmd.syncOutcome(cfg, tc.report, tc.err)
			if out.Status != tc.status || failed != tc.failed {
				t.Fatalf("out=%+v failed=%v", out, failed)
			}
			if out.Embeddings != tc.embeddings {
				t.Fatalf("embeddings=%q want %q (out=%+v)", out.Embeddings, tc.embeddings, out)
			}
			if out.Embeddings == "unavailable" && out.EmbeddingReason == "" {
				t.Fatalf("unavailable embeddings must carry a reason: %+v", out)
			}
			if out.Indexed != tc.report.Indexed {
				t.Fatal("index counts lost")
			}
		})
	}
}
