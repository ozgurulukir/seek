package indexer

import (
	"strings"
	"testing"

	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/store"
)

func TestBackendRefusal(t *testing.T) {
	offlineCfg := &config.AppConfig{CacheDir: t.TempDir()}
	offlineCfg.Config.Privacy.OfflineOnly = true
	offlineCfg.Config.Extractor.XbergBaseURL = "http://127.0.0.1:8000"

	if err := BackendRefusal(offlineCfg, "xberg"); err == nil {
		t.Fatal("xberg must be refused under offline_only")
	} else if !strings.Contains(err.Error(), "offline_only") {
		t.Errorf("unexpected refusal: %v", err)
	}

	// The refusal is deterministic: builtin, empty, and an online config are
	// all allowed.
	for _, backend := range []string{"builtin", ""} {
		if err := BackendRefusal(offlineCfg, backend); err != nil {
			t.Errorf("BackendRefusal(offline, %q) = %v, want nil", backend, err)
		}
	}
	onlineCfg := &config.AppConfig{CacheDir: t.TempDir()}
	onlineCfg.Config.Extractor.XbergBaseURL = "http://127.0.0.1:8000"
	if err := BackendRefusal(onlineCfg, "xberg"); err != nil {
		t.Errorf("BackendRefusal(online, xberg) = %v, want nil", err)
	}
}

func TestOverrideResolver(t *testing.T) {
	cfg := &config.AppConfig{CacheDir: t.TempDir()}
	resolver, err := NewOverrideResolver(cfg, "builtin")
	if err != nil {
		t.Fatalf("NewOverrideResolver: %v", err)
	}

	// The override ignores the collection: it wins even when the collection
	// row names a different backend.
	col := &store.Collection{Backend: "xberg"}
	ext1, err := resolver.Resolve(col)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	ext2, err := resolver.Resolve(nil)
	if err != nil {
		t.Fatalf("Resolve(nil): %v", err)
	}
	if ext1 != ext2 {
		t.Error("override resolver must cache the same extractor instance")
	}

	// Nil config is a construction-time error.
	if _, err := NewOverrideResolver(nil, "builtin"); err == nil {
		t.Error("nil config must fail construction")
	}
}

func TestOverrideResolver_LazyRefusal(t *testing.T) {
	cfg := &config.AppConfig{CacheDir: t.TempDir()}
	cfg.Config.Privacy.OfflineOnly = true
	cfg.Config.Extractor.XbergBaseURL = "http://127.0.0.1:8000"

	// Construction succeeds (lazy); the deterministic refusal surfaces at
	// first Resolve.
	resolver, err := NewOverrideResolver(cfg, "xberg")
	if err != nil {
		t.Fatalf("construction must be lazy, got: %v", err)
	}
	if _, err := resolver.Resolve(nil); err == nil || !strings.Contains(err.Error(), "offline_only") {
		t.Errorf("Resolve must surface the offline refusal, got: %v", err)
	}
}
