package cmd

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/ozgurulukir/seek/internal/config"
)

// DoctorCmd inspects the local installation and can repair common issues.
// It currently audits and tightens filesystem permissions on seek's private
// data (config dir, cache/index dir): the index contains the searchable text
// of every indexed note, conversation, and code file, so 0644/0755 defaults
// from older versions leak it to other local users.
type DoctorCmd struct {
	FixPermissions bool `help:"Tighten private data permissions (dirs 0700, files 0600)." xor:"action"`
}

// privatePath is one seek-owned path with the permissions it should have.
type privatePath struct {
	path    string
	dir     bool
	wantMod fs.FileMode
}

// privatePaths enumerates seek's private-data locations. Only real seek state
// is included: the config file/dir and the cache dir with the SQLite index
// (plus WAL/SHM sidecars, which hold the same content while live). db_path
// and cache_dir may be relocated via config, so the effective paths come from
// the loaded config, not just the defaults.
func privatePaths(cfg *config.AppConfig) []privatePath {
	paths := []privatePath{
		{config.ConfigDir(), true, config.DefaultPrivateDirPerms},
		{filepath.Join(config.ConfigDir(), "config.yaml"), false, config.DefaultPrivateFilePerms},
		{cfg.CacheDir, true, config.DefaultPrivateDirPerms},
		{cfg.DBPath, false, config.DefaultPrivateFilePerms},
		{cfg.DBPath + "-wal", false, config.DefaultPrivateFilePerms},
		{cfg.DBPath + "-shm", false, config.DefaultPrivateFilePerms},
	}
	return paths
}

// checkPermissions returns the paths whose mode leaks to group/other. Only
// paths with group/other bits set are reported — stricter-than-wanted modes
// (e.g. a 0400 config) are deliberately left alone: repair must never widen
// any permission, only remove group/other access.
func checkPermissions(cfg *config.AppConfig) ([]privatePath, error) {
	var loose []privatePath
	for _, p := range privatePaths(cfg) {
		fi, err := os.Stat(p.path)
		if err != nil {
			if os.IsNotExist(err) {
				continue // nothing to audit for absent files (e.g. no WAL yet)
			}
			return nil, fmt.Errorf("stat %s: %w", p.path, err)
		}
		if fi.Mode().Perm()&0o077 != 0 {
			loose = append(loose, p)
		}
	}
	return loose, nil
}

// fixPermissions tightens the listed paths to their expected mode. It only
// ever removes group/other access — never widens anything.
func fixPermissions(paths []privatePath) ([]string, []error) {
	var fixed []string
	var errs []error
	for _, p := range paths {
		if err := os.Chmod(p.path, p.wantMod); err != nil {
			errs = append(errs, fmt.Errorf("chmod %s: %w", p.path, err))
			continue
		}
		fixed = append(fixed, fmt.Sprintf("%s -> %04o", p.path, uint32(p.wantMod.Perm())))
	}
	return fixed, errs
}

func (c *DoctorCmd) Run(cfg *config.AppConfig) error {
	loose, err := checkPermissions(cfg)
	if err != nil {
		return err
	}

	if len(loose) == 0 {
		fmt.Println("permissions OK: private data is owner-only (dirs 0700, files 0600)")
		return nil
	}

	fmt.Printf("found %d path(s) with loose permissions:\n", len(loose))
	for _, p := range loose {
		mode := "0600"
		if p.dir {
			mode = "0700"
		}
		fmt.Printf("  %s (want %s)\n", p.path, mode)
	}

	if !c.FixPermissions {
		fmt.Println("\nrun with --fix-permissions to tighten them (only removes group/other access)")
		return nil
	}

	fixed, errs := fixPermissions(loose)
	sort.Strings(fixed)
	for _, f := range fixed {
		fmt.Println("fixed:", f)
	}
	if len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintln(os.Stderr, "error:", e)
		}
		return fmt.Errorf("%d path(s) could not be tightened", len(errs))
	}
	fmt.Println("\npermissions repaired: private data is now owner-only")
	return nil
}
