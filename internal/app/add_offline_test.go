package app

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/store"
	"github.com/ozgurulukir/seek/internal/store/storetest"
)

// TestAddRefusesOfflineXbergBackend pins the fail-fast pre-flight: a
// documents add with --backend xberg under privacy.offline_only must error
// before any collection row is created. Degrading instead used to persist a
// collection whose row said xberg while the initial sync ran builtin, and
// every later seek sync then failed permanently.
func TestAddRefusesOfflineXbergBackend(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		storetest.SkipIfFTS5Disabled(t, err)
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	cfg := &config.AppConfig{CacheDir: t.TempDir()}
	cfg.Config.Privacy.OfflineOnly = true
	cfg.Config.Extractor.XbergBaseURL = "http://127.0.0.1:8000"

	root := t.TempDir()
	svc := NewAddCollectionService(db, cfg)
	req, err := BuildAddRequest(AddFlags{Path: root, Name: "docs", Type: "documents", Backend: "xberg"})
	if err != nil {
		t.Fatalf("BuildAddRequest: %v", err)
	}
	if err := svc.Add(req); err == nil {
		t.Fatal("add must refuse --backend xberg under offline_only")
	} else if !strings.Contains(err.Error(), "offline_only") {
		t.Errorf("unexpected error: %v", err)
	}
	if _, err := db.GetCollectionByName("docs"); err == nil {
		t.Error("no collection row must be created when the backend is refused")
	}

	// The same add with builtin succeeds offline.
	req, err = BuildAddRequest(AddFlags{Path: root, Name: "docs2", Type: "documents", Backend: "builtin"})
	if err != nil {
		t.Fatalf("BuildAddRequest builtin: %v", err)
	}
	if err := svc.Add(req); err != nil {
		t.Errorf("documents add with --backend builtin must succeed offline: %v", err)
	}
	if _, err := db.GetCollectionByName("docs2"); err != nil {
		t.Errorf("builtin collection must be created: %v", err)
	}
}
