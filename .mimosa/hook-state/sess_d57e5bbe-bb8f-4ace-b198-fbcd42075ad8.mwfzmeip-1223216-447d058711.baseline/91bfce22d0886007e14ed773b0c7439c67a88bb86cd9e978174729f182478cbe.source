package indexer

import (
	"strings"
	"testing"

	"github.com/ozgurulukir/seek/internal/config"
)

func TestNewExtractor_XbergRefusedWhenOfflineOnly(t *testing.T) {
	cfg := &config.AppConfig{CacheDir: t.TempDir()}
	cfg.Config.Privacy.OfflineOnly = true
	cfg.Config.Extractor.XbergBaseURL = "http://127.0.0.1:8000"

	_, err := NewExtractor(cfg, "xberg")
	if err == nil {
		t.Fatal("xberg backend constructed while offline_only is enabled")
	}
	if !strings.Contains(err.Error(), "offline_only") || !strings.Contains(err.Error(), "xberg") {
		t.Errorf("error should name offline_only and xberg, got: %v", err)
	}

	// builtin stays available in the same mode.
	if _, err := NewExtractor(cfg, "builtin"); err != nil {
		t.Errorf("builtin should remain available offline: %v", err)
	}
}
