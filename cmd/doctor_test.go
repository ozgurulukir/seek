package cmd

import (
	"os"
	"path/filepath"
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
	for _, file := range []string{cfgPath, dbPath, dbPath + "-wal"} {
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
	// 5 loose paths: 2 dirs + config.yaml + db + wal (fixture creates all;
	// privatePaths also lists the -shm sidecar, absent in this fixture).
	if len(loose) != 5 {
		t.Fatalf("got %d loose paths (%v), want 5", len(loose), loose)
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
	if len(fixed) != 5 {
		t.Fatalf("fixed %d paths, want 5", len(fixed))
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
	for _, path := range []string{cfg.DBPath, cfg.DBPath + "-wal"} {
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
