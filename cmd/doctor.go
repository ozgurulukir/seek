package cmd

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ozgurulukir/seek/internal/app"
	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/embed"
	"github.com/ozgurulukir/seek/internal/store"
)

// DoctorCmd inspects the local installation and can repair common issues.
// It currently audits and tightens filesystem permissions on seek's private
// data (config dir, cache/index dir): the index contains the searchable text
// of every indexed note, conversation, and code file, so 0644/0755 defaults
// from older versions leak it to other local users.
type DoctorCmd struct {
	FixPermissions bool `help:"Tighten private data permissions (dirs 0700, files 0600)." xor:"action"`
	Verbose        bool `help:"Also print the effective resolved config (advanced knobs + applied defaults)"`
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
	c.reportEmbedding(cfg)
	c.reportServices(cfg)
	// The effective-defaults block is opt-in via --verbose; when unset the
	// output below is byte-for-byte unchanged.
	if c.Verbose {
		c.reportEffectiveDefaults(cfg)
	}

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

// reportEmbedding prints the embedding mode, provider capability, active
// profile, and any config/profile divergence. It opens the store read-only to
// read the persisted profile; a missing profile is reported, never created.
func (c *DoctorCmd) reportEmbedding(cfg *config.AppConfig) {
	fmt.Println("embedding:")
	ec := cfg.Config.Embedding
	if ec.BaseURL == "" || ec.Model == "" {
		fmt.Println("  not configured (set embedding.base_url/model/api_key)")
		return
	}

	mode := ec.EffectiveMode()
	fmt.Printf("  mode: %s (config embedding.mode=%q; resolves to %s)\n", mode, ec.Mode, modeLabel(mode))

	caps := embed.ProviderCapabilities(cfg)
	fmt.Printf("  capability: realtime=%v async_batch=%v (provider kind %s)\n",
		caps.RealtimeEmbeddings, caps.AsyncBatch, embed.DetectProviderKind(cfg))

	db, err := app.OpenStore(cfg)
	if err != nil {
		fmt.Printf("  profile: (unreadable: %v)\n", err)
		return
	}
	defer db.Close()

	stored, err := db.GetEmbeddingProfile(context.Background())
	if err != nil {
		fmt.Printf("  profile: (unreadable: %v)\n", err)
		return
	}
	desired := store.ProfileFromConfig(cfg)
	if stored == nil {
		fmt.Printf("  profile: none (run: seek embed)\n")
		return
	}
	if stored.Fingerprint == desired.ComputeFingerprint() {
		fmt.Printf("  profile: %s/%d, ready\n", stored.Model, stored.Dimensions)
		return
	}
	fmt.Printf("  profile: %s/%d, STALE — config wants %s/%d\n", stored.Model, stored.Dimensions, desired.Model, desired.Dimensions)
	fmt.Printf("  fix: reindex with: seek rm <collection> && seek add && seek embed -f\n")
}

// reportServices prints the optional-services matrix: each of reranker,
// semantic, OCR/VL, and xberg is classified as disabled|ready|unavailable|
// blocked from the resolved config (config.ServiceStatuses). It is additive to
// the privacy and embedding reports and runs on the default (non-verbose) path,
// so "seek doctor" reports every optional service's status.
//
// Readiness is config-derived — no endpoint is probed — so the matrix is fast
// and deterministic. The core keyword flow (BM25/FTS search; markdown/code/
// pdf/images add+sync) needs none of these; xberg is the one exception, and
// only when it is explicitly selected (extractor.backend=xberg / --backend
// xberg), which is an explicit user request rather than a core-flow dependency.
func (c *DoctorCmd) reportServices(cfg *config.AppConfig) {
	fmt.Println("optional services:")
	for _, s := range config.ServiceStatuses(cfg) {
		fmt.Printf("  %-8s %-11s %s\n", s.Name, string(s.Status), s.Detail)
	}
}

// reportEffectiveDefaults prints the resolved config grouped by profile,
// including the advanced-only knobs (vector index / compression) and every
// default that applyFallbacks filled in. It is the --verbose half of doctor and
// never runs on the default (non-verbose) path.
func (c *DoctorCmd) reportEffectiveDefaults(cfg *config.AppConfig) {
	fmt.Println("\neffective config (resolved values incl. advanced knobs + applied defaults):")
	for _, p := range config.Group(cfg, true) {
		fmt.Printf("  %s — %s\n", p.Title, p.Summary)
		for _, block := range p.Sections {
			for _, line := range strings.Split(strings.TrimRight(block, "\n"), "\n") {
				fmt.Printf("    %s\n", line)
			}
		}
	}
}

// modeLabel maps a config embedding.mode value to its resolved behavior.
func modeLabel(mode string) string {
	switch mode {
	case config.ModeRealtime:
		return "realtime request batch"
	case config.ModeBatch:
		return "async provider batch"
	default:
		return "realtime request batch (auto)"
	}
}
