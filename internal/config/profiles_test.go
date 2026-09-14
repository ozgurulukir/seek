package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sectionText concatenates every YAML block across the given profiles so tests
// can assert on which keys are (or are not) visible.
func sectionText(profiles []ProfileView) string {
	var b strings.Builder
	for _, p := range profiles {
		for _, s := range p.Sections {
			b.WriteString(s)
		}
	}
	return b.String()
}

// TestGroup_HidesAdvancedByDefault pins the core C5 visibility contract: the
// plain view (advanced=false) hides vector_index and compression, while the
// advanced view (advanced=true) shows them.
func TestGroup_HidesAdvancedByDefault(t *testing.T) {
	cfg := &AppConfig{
		CacheDir: "/tmp/seek-cache",
		DBPath:   "/tmp/seek-cache/index.db",
		Config: Config{
			Search:      SearchConfig{DefaultLimit: 5},
			VectorIndex: VectorIndexConfig{Backend: "linear", HNSW: HNSWConfig{M: 32, EFSearch: 200}},
			Compression: CompressionConfig{Algorithm: "zstd", Level: 9},
		},
	}

	plain := sectionText(Group(cfg, false))
	if strings.Contains(plain, "vector_index:") {
		t.Error("plain view must hide vector_index")
	}
	if strings.Contains(plain, "compression:") {
		t.Error("plain view must hide compression")
	}
	// The user-facing sections are still present.
	for _, want := range []string{"search:", "default_limit: 5"} {
		if !strings.Contains(plain, want) {
			t.Errorf("plain view missing %q", want)
		}
	}

	adv := Group(cfg, true)
	advText := sectionText(adv)
	if !strings.Contains(advText, "vector_index:") {
		t.Error("advanced view must show vector_index")
	}
	if !strings.Contains(advText, "compression:") {
		t.Error("advanced view must show compression")
	}
	// Advanced view must expose the actual tuned values, not just the key.
	if !strings.Contains(advText, "backend: linear") {
		t.Error("advanced view should show backend: linear")
	}
	if !strings.Contains(advText, "level: 9") {
		t.Error("advanced view should show level: 9")
	}
}

// TestGroup_HasAdvancedProfileOnlyWhenRequested pins that the "advanced"
// profile is absent from the plain view and present in the advanced view.
func TestGroup_HasAdvancedProfileOnlyWhenRequested(t *testing.T) {
	cfg := &AppConfig{Config: Config{Compression: CompressionConfig{Algorithm: "zstd", Level: 3}}}

	var plainHasAdvanced, advHasAdvanced bool
	for _, p := range Group(cfg, false) {
		if p.ID == "advanced" {
			plainHasAdvanced = true
		}
	}
	for _, p := range Group(cfg, true) {
		if p.ID == "advanced" {
			advHasAdvanced = true
		}
	}
	if plainHasAdvanced {
		t.Error("advanced profile must not appear in the plain view")
	}
	if !advHasAdvanced {
		t.Error("advanced profile must appear in the advanced view")
	}
}

// TestGroup_CoversEverySection ensures no config section is dropped by the
// profile split: every top-level Config key must appear in some profile when
// the advanced view is requested. This preserves the full config schema in the
// presentation (plan §6: "mevcut 11 bölümlü config yapısını koru").
func TestGroup_CoversEverySection(t *testing.T) {
	cfg := &AppConfig{
		CacheDir: "/tmp/seek-cache",
		DBPath:   "/tmp/seek-cache/index.db",
		Config: Config{
			Embedding:    EmbeddingConfig{BaseURL: "https://x/v1", Model: "m"},
			Chunk:        ChunkConfig{MaxSize: 1000},
			OCR:          OCRConfig{Enabled: true},
			Rerank:       RerankConfig{Enabled: true},
			Search:       SearchConfig{DefaultLimit: 5},
			Filters:      FilterConfig{Enabled: true},
			Aggregations: AggregationConfig{Enabled: true},
			VectorIndex:  VectorIndexConfig{Backend: "hnsw"},
			Compression:  CompressionConfig{Algorithm: "zstd"},
			Extractor:    ExtractorConfig{Backend: "builtin"},
			Privacy:      PrivacyConfig{OfflineOnly: true},
			Semantic:     SemanticConfig{Enabled: true},
		},
	}

	// Every section key must be rendered somewhere in the advanced view.
	covered := map[string]bool{}
	for _, p := range Group(cfg, true) {
		for _, line := range strings.Split(sectionText([]ProfileView{p}), "\n") {
			key := strings.TrimSuffix(strings.TrimSpace(line), ":")
			if key != "" && !strings.Contains(key, " ") {
				covered[key] = true
			}
		}
	}
	// "paths" is a runtime section, not a Config key; exclude it from the check.
	delete(covered, "paths")
	want := []string{
		"embedding", "chunk", "ocr", "rerank", "search", "filters",
		"aggregations", "vector_index", "compression", "extractor",
		"privacy", "semantic",
	}
	for _, k := range want {
		if !covered[k] {
			t.Errorf("section %q is not covered by any profile (dropped from presentation)", k)
		}
	}
}

// TestPlainViewDoesNotAlterConfigFile is the C5 round-trip guarantee (plan §6:
// "bilinenmeyen/ileri knob'lar sade UI kullanırken kaybolmaz"). The plain view
// is a read-only projection: reading a config that carries advanced + unknown
// knobs and rendering it via the plain view must leave the on-disk file
// byte-for-byte intact, so nothing is silently dropped or overwritten.
func TestPlainViewDoesNotAlterConfigFile(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	cfgDir := filepath.Join(tmpHome, ".config", "seek")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	cfgPath := filepath.Join(cfgDir, "config.yaml")

	// A config carrying advanced knobs (vector_index, compression) AND an
	// unknown future section that the schema does not recognize.
	original := []byte("embedding:\n" +
		"  base_url: http://127.0.0.1:11434/v1\n" +
		"  model: nomic-embed-text\n" +
		"  dimensions: 768\n" +
		"vector_index:\n" +
		"  backend: linear\n" +
		"  hnsw:\n" +
		"    m: 32\n" +
		"    ef_search: 200\n" +
		"compression:\n" +
		"  algorithm: zstd\n" +
		"  level: 9\n" +
		"# unknown future knob — must survive on disk verbatim\n" +
		"future_section:\n" +
		"  keep_me: true\n")
	if err := os.WriteFile(cfgPath, original, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// Load exactly as main.go does, then render via the plain view.
	ac, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	_ = Group(ac, false) // plain view projection (read-only)

	// The file on disk must be byte-for-byte unchanged.
	onDisk, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config back: %v", err)
	}
	if string(onDisk) != string(original) {
		t.Fatalf("plain view altered the config file:\n--- original ---\n%s\n--- on disk ---\n%s", original, onDisk)
	}

	// The advanced + unknown knobs must still be present on disk (not dropped).
	s := string(onDisk)
	for _, want := range []string{"backend: linear", "algorithm: zstd", "level: 9", "keep_me: true"} {
		if !strings.Contains(s, want) {
			t.Errorf("advanced/unknown knob %q was lost from the config file", want)
		}
	}
}

// TestHasAdvancedKnobs pins the helper the plain view uses to nudge the user.
func TestHasAdvancedKnobs(t *testing.T) {
	if HasAdvancedKnobs(Config{}) {
		t.Error("empty config must have no advanced knobs")
	}
	if !HasAdvancedKnobs(Config{VectorIndex: VectorIndexConfig{Backend: "linear"}}) {
		t.Error("a set vector_index counts as an advanced knob")
	}
	if !HasAdvancedKnobs(Config{Compression: CompressionConfig{Algorithm: "zstd", Level: 3}}) {
		t.Error("a set compression counts as an advanced knob")
	}
}
