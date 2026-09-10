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
// the loaded config, not just the defaults. The HNSW vector index is covered
// too: its persist_path is configurable and defaults under cache_dir.
func privatePaths(cfg *config.AppConfig) []privatePath {
	hnswPath := cfg.Config.VectorIndex.HNSW.PersistPath
	if hnswPath == "" {
		hnswPath = filepath.Join(cfg.CacheDir, "hnsw.index")
	}
	paths := []privatePath{
		{config.ConfigDir(), true, config.DefaultPrivateDirPerms},
		{filepath.Join(config.ConfigDir(), "config.yaml"), false, config.DefaultPrivateFilePerms},
		{cfg.CacheDir, true, config.DefaultPrivateDirPerms},
		{cfg.DBPath, false, config.DefaultPrivateFilePerms},
		{cfg.DBPath + "-wal", false, config.DefaultPrivateFilePerms},
		{cfg.DBPath + "-shm", false, config.DefaultPrivateFilePerms},
		{hnswPath, false, config.DefaultPrivateFilePerms},
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
	c.reportPrivacy(cfg)

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

// reportPrivacy prints which external endpoints receive which data types —
// the disclosure surface for seek's network egress paths (keyword search is
// local; external embedding/rerank/OCR are not allowed in offline-only mode).
func (c *DoctorCmd) reportPrivacy(cfg *config.AppConfig) {
	fmt.Println("privacy / data egress:")
	if cfg.Config.OfflineOnly() {
		fmt.Println("  offline_only: true — external embedding, rerank, OCR, and xberg calls are blocked; loopback OCR is allowed")
	} else {
		fmt.Println("  offline_only: false — external providers may receive data:")
	}
	ec := cfg.Config.Embedding
	if ec.BaseURL != "" && ec.Model != "" {
		fmt.Printf("  embedding: %s (%s)  <- document/query text, images (multimodal)\n", ec.BaseURL, ec.Model)
	}
	if cfg.Config.Rerank.Enabled && cfg.Config.Rerank.BaseURL != "" {
		fmt.Printf("  rerank:    %s (%s)  <- query + result text\n", cfg.Config.Rerank.BaseURL, cfg.Config.Rerank.Model)
	}
	if cfg.Config.OCR.Enabled && cfg.Config.OCR.BaseURL != "" {
		status := ""
		if cfg.Config.OfflineOnly() {
			if cfg.Config.CanUseOCR() {
				status = " [loopback allowed]"
			} else {
				status = " [blocked by offline_only]"
			}
		}
		fmt.Printf("  ocr:       %s (%s)%s  <- scanned PDF page images\n", cfg.Config.OCR.BaseURL, cfg.Config.OCR.Model, status)
	}
	if cfg.Config.Extractor.XbergBaseURL != "" && !cfg.Config.OfflineOnly() {
		fmt.Printf("  xberg:     %s  <- full document contents (rich-format extraction)\n", cfg.Config.Extractor.XbergBaseURL)
	}
	if cfg.Config.OfflineOnly() {
		return
	}
	if ec.BaseURL == "" {
		fmt.Println("  (no embedding endpoint configured)")
	}
	fmt.Println("  keyword (BM25/FTS) search is fully local; no telemetry is sent anywhere")
}
