package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ozgurulukir/seek/internal/config"
)

// doctorFixture builds a fake seek installation with deliberately loose
// permissions and an AppConfig pointing at it. HOME/USERPROFILE are pointed
// at the same temp root so config.ConfigDir()/Load-style global helpers also
// resolve inside the fixture.
func doctorFixture(t *testing.T) (*config.AppConfig, string) {
	t.Helper()
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	cfgDir := filepath.Join(tmpHome, ".config", "seek")
	cacheDir := filepath.Join(tmpHome, ".cache", "seek")
	for _, dir := range []string{cfgDir, cacheDir} {
		if err := os.MkdirAll(dir, 0755); err != nil { // loose on purpose
			t.Fatal(err)
		}
	}
	cfgPath := filepath.Join(cfgDir, "config.yaml")
	dbPath := filepath.Join(cacheDir, "index.db")
	hnswPath := filepath.Join(cacheDir, "hnsw.index")
	for _, file := range []string{cfgPath, dbPath, dbPath + "-wal", hnswPath} {
		if err := os.WriteFile(file, []byte("data"), 0644); err != nil { // loose on purpose
			t.Fatal(err)
		}
	}
	cfg := &config.AppConfig{
		CacheDir: cacheDir,
		DBPath:   dbPath,
	}
	return cfg, cfgDir
}

func TestDoctor_CheckPermissionsFindsLoose(t *testing.T) {
	cfg, _ := doctorFixture(t)

	loose, err := checkPermissions(cfg)
	if err != nil {
		t.Fatalf("checkPermissions: %v", err)
	}
	// 6 loose paths: 2 dirs + config.yaml + db + wal + hnsw.index (fixture
	// creates all; the -shm sidecar is also listed but absent in this
	// fixture, so it is skipped as missing).
	if len(loose) != 6 {
		t.Fatalf("got %d loose paths (%v), want 6", len(loose), loose)
	}
}

func TestDoctor_HNSWPersistPathHonored(t *testing.T) {
	cfg, _ := doctorFixture(t)
	// Relocate the HNSW index; checkPermissions must follow the configured
	// path, not the default under cache_dir.
	custom := filepath.Join(t.TempDir(), "elsewhere.index")
	if err := os.WriteFile(custom, []byte("v"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg.Config.VectorIndex.HNSW.PersistPath = custom

	loose, err := checkPermissions(cfg)
	if err != nil {
		t.Fatalf("checkPermissions: %v", err)
	}
	found := false
	for _, p := range loose {
		if p.path == custom {
			found = true
		}
	}
	if !found {
		t.Errorf("custom hnsw persist_path %q not audited; got %v", custom, loose)
	}
}

func TestDoctor_FixPermissionsTightens(t *testing.T) {
	cfg, _ := doctorFixture(t)

	loose, err := checkPermissions(cfg)
	if err != nil {
		t.Fatalf("checkPermissions: %v", err)
	}
	fixed, errs := fixPermissions(loose)
	if len(errs) != 0 {
		t.Fatalf("fixPermissions errors: %v", errs)
	}
	if len(fixed) != 6 {
		t.Fatalf("fixed %d paths, want 6", len(fixed))
	}

	// On POSIX, fixPermissions tightens the mode bits; on Windows chmod is a
	// no-op and os.Stat reports the default perm bits, so the mode assertions
	// below are meaningless there. Assert the meaningful behavior on Windows:
	// the fix left every path intact (no deletion or corruption).
	if runtime.GOOS == "windows" {
		for _, p := range loose {
			if _, err := os.Stat(p.path); err != nil {
				t.Errorf("fixPermissions broke %s: %v", p.path, err)
			}
		}
		return
	}

	for _, path := range []string{
		config.ConfigDir(),
		cfg.CacheDir,
	} {
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := fi.Mode().Perm(); got != config.DefaultPrivateDirPerms {
			t.Errorf("%s perm = %04o, want %04o", path, uint32(got), uint32(config.DefaultPrivateDirPerms))
		}
	}
	for _, path := range []string{cfg.DBPath, cfg.DBPath + "-wal", filepath.Join(cfg.CacheDir, "hnsw.index")} {
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := fi.Mode().Perm(); got != config.DefaultPrivateFilePerms {
			t.Errorf("%s perm = %04o, want %04o", path, uint32(got), uint32(config.DefaultPrivateFilePerms))
		}
	}

	// After fixing, nothing is loose anymore.
	loose, err = checkPermissions(cfg)
	if err != nil {
		t.Fatalf("checkPermissions after fix: %v", err)
	}
	if len(loose) != 0 {
		t.Errorf("still loose after fix: %v", loose)
	}
}

func TestDoctor_NeverWidensPermissions(t *testing.T) {
	cfg, cfgDir := doctorFixture(t)
	// A path already stricter than wanted must stay untouched.
	tight := filepath.Join(cfgDir, "config.yaml")
	if err := os.Chmod(tight, 0400); err != nil {
		t.Fatal(err)
	}

	loose, err := checkPermissions(cfg)
	if err != nil {
		t.Fatal(err)
	}

	// On Windows chmod is a no-op and the perm bits are not meaningful, so the
	// "stricter mode is not reported as loose / never widened" invariant cannot
	// be exercised. Assert the meaningful behavior: the fix leaves the file
	// intact and reports no errors.
	if runtime.GOOS == "windows" {
		if _, errs := fixPermissions(loose); len(errs) != 0 {
			t.Fatalf("fixPermissions errors: %v", errs)
		}
		if _, err := os.Stat(tight); err != nil {
			t.Errorf("fixPermissions broke %s: %v", tight, err)
		}
		return
	}

	for _, p := range loose {
		if p.path == tight {
			t.Error("0400 config reported as loose; repair must never widen permissions")
		}
	}
	if _, errs := fixPermissions(loose); len(errs) != 0 {
		t.Fatalf("fixPermissions errors: %v", errs)
	}
	fi, err := os.Stat(tight)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0400 {
		t.Errorf("tight file changed: %04o, want 0400", uint32(fi.Mode().Perm()))
	}
}

func TestDoctor_SkipsMissingPaths(t *testing.T) {
	cfg, _ := doctorFixture(t)
	// Remove the WAL sidecar: doctor must not error on absent files.
	if err := os.Remove(cfg.DBPath + "-wal"); err != nil {
		t.Fatal(err)
	}
	if _, err := checkPermissions(cfg); err != nil {
		t.Fatalf("checkPermissions with missing sidecar: %v", err)
	}
}

func TestDoctor_RunReportsLooseWithoutFix(t *testing.T) {
	cfg, _ := doctorFixture(t)
	cmd := &DoctorCmd{}
	// Run's output goes to stdout; assert no error and that the loose state
	// survives (no fix without the flag).
	if err := cmd.Run(cfg); err != nil {
		t.Fatalf("DoctorCmd.Run without --fix-permissions: %v", err)
	}
	loose, err := checkPermissions(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(loose) == 0 {
		t.Error("permissions were changed without --fix-permissions")
	}
}

func TestDoctor_PrivacyReportOffline(t *testing.T) {
	cfg, _ := doctorFixture(t)
	cfg.Config.Privacy.OfflineOnly = true
	cfg.Config.Embedding.BaseURL = "https://api.example.com/v1"
	cfg.Config.Embedding.Model = "m"
	// Doctor must run cleanly in offline mode and not create any client.
	cmd := &DoctorCmd{FixPermissions: true}
	if err := cmd.Run(cfg); err != nil {
		t.Fatalf("DoctorCmd.Run offline: %v", err)
	}
}

// statusFor returns the status of one service row by its stable key.
func statusFor(t *testing.T, cfg *config.AppConfig, key string) config.ServiceStatus {
	t.Helper()
	for _, r := range config.ServiceStatuses(cfg) {
		if r.Key == key {
			return r.Status
		}
	}
	t.Fatalf("service %q not found in %v", key, cfg)
	return ""
}

// TestDoctor_ServiceStatusesDefaultAllDisabled verifies a fresh config (no
// optional service enabled) reports every helper as disabled, in the fixed
// order reranker, semantic, ocr/vl, xberg.
func TestDoctor_ServiceStatusesDefaultAllDisabled(t *testing.T) {
	cfg, _ := doctorFixture(t)
	got := config.ServiceStatuses(cfg)
	if len(got) != 4 {
		t.Fatalf("got %d services, want 4", len(got))
	}
	wantKeys := []string{"reranker", "semantic", "ocr-vl", "xberg"}
	for i, r := range got {
		if r.Key != wantKeys[i] {
			t.Errorf("service[%d] key = %q, want %q", i, r.Key, wantKeys[i])
		}
		if r.Status != config.ServiceDisabled {
			t.Errorf("service %q default status = %q, want disabled", r.Key, r.Status)
		}
	}
}

func TestDoctor_ServiceStatusesReranker(t *testing.T) {
	cfg, _ := doctorFixture(t)
	cfg.Config.Rerank.Enabled = true
	cfg.Config.Rerank.BaseURL = "https://rerank.example.com/v1"

	if s := statusFor(t, cfg, "reranker"); s != config.ServiceReady {
		t.Errorf("rerank remote, offline off = %q, want ready", s)
	}
	cfg.Config.Privacy.OfflineOnly = true
	if s := statusFor(t, cfg, "reranker"); s != config.ServiceBlocked {
		t.Errorf("rerank remote, offline on = %q, want blocked", s)
	}

	// Loopback reranker is allowed under offline_only.
	cfg.Config.Privacy.OfflineOnly = false
	cfg.Config.Rerank.BaseURL = "http://127.0.0.1:8010"
	if s := statusFor(t, cfg, "reranker"); s != config.ServiceReady {
		t.Errorf("rerank loopback, offline off = %q, want ready", s)
	}
	cfg.Config.Privacy.OfflineOnly = true
	if s := statusFor(t, cfg, "reranker"); s != config.ServiceReady {
		t.Errorf("rerank loopback, offline on = %q, want ready", s)
	}
}

func TestDoctor_ServiceStatusesSemantic(t *testing.T) {
	cfg, _ := doctorFixture(t)
	cfg.Config.Semantic.Enabled = true
	cfg.Config.Semantic.BaseURL = "http://127.0.0.1:8003" // loopback

	if s := statusFor(t, cfg, "semantic"); s != config.ServiceReady {
		t.Errorf("semantic loopback, offline off = %q, want ready", s)
	}
	cfg.Config.Privacy.OfflineOnly = true
	if s := statusFor(t, cfg, "semantic"); s != config.ServiceReady {
		t.Errorf("semantic loopback, offline on = %q, want ready", s)
	}

	// Remote semantic endpoint.
	cfg.Config.Privacy.OfflineOnly = false
	cfg.Config.Semantic.BaseURL = "https://semantic.example.com"
	if s := statusFor(t, cfg, "semantic"); s != config.ServiceReady {
		t.Errorf("semantic remote, offline off = %q, want ready", s)
	}
	cfg.Config.Privacy.OfflineOnly = true
	if s := statusFor(t, cfg, "semantic"); s != config.ServiceBlocked {
		t.Errorf("semantic remote, offline on = %q, want blocked", s)
	}
}

func TestDoctor_ServiceStatusesOCRVL(t *testing.T) {
	cfg, _ := doctorFixture(t)

	// OCR enabled, remote endpoint, offline off → ready.
	cfg.Config.OCR.Enabled = true
	cfg.Config.OCR.APIKey = "key"
	cfg.Config.OCR.BaseURL = "https://ocr.example.com"
	if s := statusFor(t, cfg, "ocr-vl"); s != config.ServiceReady {
		t.Errorf("ocr remote, offline off = %q, want ready", s)
	}
	cfg.Config.Privacy.OfflineOnly = true
	if s := statusFor(t, cfg, "ocr-vl"); s != config.ServiceBlocked {
		t.Errorf("ocr remote, offline on = %q, want blocked", s)
	}

	// OCR loopback under offline_only → ready.
	cfg.Config.Privacy.OfflineOnly = false
	cfg.Config.OCR.BaseURL = "http://127.0.0.1:9000"
	cfg.Config.Privacy.OfflineOnly = true
	if s := statusFor(t, cfg, "ocr-vl"); s != config.ServiceReady {
		t.Errorf("ocr loopback, offline on = %q, want ready", s)
	}

	// VL (multimodal) enabled, remote default endpoint.
	cfg.Config.Privacy.OfflineOnly = false
	cfg.Config.OCR.Enabled = false
	cfg.Config.OCR.APIKey = ""
	cfg.Config.Embedding.Multimodal = true
	cfg.Config.Embedding.APIKey = "key"
	if s := statusFor(t, cfg, "ocr-vl"); s != config.ServiceReady {
		t.Errorf("vl remote, offline off = %q, want ready", s)
	}
	cfg.Config.Privacy.OfflineOnly = true
	if s := statusFor(t, cfg, "ocr-vl"); s != config.ServiceBlocked {
		t.Errorf("vl remote, offline on = %q, want blocked", s)
	}
}

func TestDoctor_ServiceStatusesXberg(t *testing.T) {
	cfg, _ := doctorFixture(t)
	cfg.Config.Extractor.Backend = "xberg"
	cfg.Config.Extractor.XbergBaseURL = "http://127.0.0.1:8000"

	if s := statusFor(t, cfg, "xberg"); s != config.ServiceReady {
		t.Errorf("xberg, offline off = %q, want ready", s)
	}
	cfg.Config.Privacy.OfflineOnly = true
	if s := statusFor(t, cfg, "xberg"); s != config.ServiceBlocked {
		t.Errorf("xberg, offline on = %q, want blocked", s)
	}
}

// TestDoctor_ServiceStatusesXbergDependencyNote documents the explicit-xberg
// nuance: when xberg is selected and reachable, its detail states the
// documents add/sync dependency; when it is not selected, it stays disabled.
func TestDoctor_ServiceStatusesXbergDependencyNote(t *testing.T) {
	cfg, _ := doctorFixture(t)

	// Not selected → disabled, and the detail names the active backend.
	cfg.Config.Extractor.Backend = "builtin"
	for _, r := range config.ServiceStatuses(cfg) {
		if r.Key == "xberg" && !strings.Contains(r.Detail, "builtin") {
			t.Errorf("xberg disabled detail = %q, want it to mention builtin", r.Detail)
		}
	}

	// Selected and reachable → ready, and the detail flags the dependency.
	cfg.Config.Extractor.Backend = "xberg"
	for _, r := range config.ServiceStatuses(cfg) {
		if r.Key == "xberg" && !strings.Contains(r.Detail, "dependency") {
			t.Errorf("xberg ready detail = %q, want it to flag the explicit dependency", r.Detail)
		}
	}
}

// TestDoctor_ServiceStatusesNeverProbes ensures the classification runs without
// constructing clients or touching the network: a plain Run must succeed even
// with xberg selected (which would otherwise try to health-check a server).
func TestDoctor_RunReportsServices(t *testing.T) {
	cfg, _ := doctorFixture(t)
	cfg.Config.Extractor.Backend = "xberg"
	cmd := &DoctorCmd{}
	if err := cmd.Run(cfg); err != nil {
		t.Fatalf("DoctorCmd.Run with xberg selected: %v", err)
	}
}
