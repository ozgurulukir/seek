package config

import (
	"bytes"
	"strings"

	"gopkg.in/yaml.v3"
)

// ProfileView is one profile group shown by `seek config`. Each profile maps to
// a set of config sections; Sections holds the YAML blocks (already indented)
// so the rendered output is valid, editable config.
type ProfileView struct {
	// ID is the stable profile identifier (core, local-semantic, ...).
	ID string
	// Title is the human-facing profile name shown in output.
	Title string
	// Summary is a one-line description of what the profile covers.
	Summary string
	// Sections is the ordered list of YAML blocks for this profile.
	Sections []string
}

// profileOrder is the order profiles are displayed. "advanced" is emitted only
// when Group is asked for the advanced view (see the advanced flag).
var profileOrder = []string{"core", "local-semantic", "document-extras", "advanced"}

// profileSections maps each profile to the config YAML keys it exposes. The
// mapping is presentation-only: it never renames, drops, or rewrites the
// config schema. "advanced" keys (vector_index, compression) are advanced-only
// visibility and are only rendered when advanced is true.
var profileSections = map[string][]string{
	"core":            {"search", "filters", "aggregations", "privacy", "chunk"},
	"local-semantic":  {"embedding", "rerank", "semantic"},
	"document-extras": {"ocr", "extractor"},
	"advanced":        {"vector_index", "compression"},
}

// profileSummary holds the one-line description shown under each profile title.
var profileSummary = map[string]string{
	"core":            "DB, source, search, and privacy",
	"local-semantic":  "embedding, rerank, and semantic tagging",
	"document-extras": "OCR, multimodal (VL), and document extraction (xberg)",
	"advanced":        "Advanced-only knobs (not written to a fresh default config)",
}

// configPaths mirrors the runtime paths reported for the core profile. They are
// computed by Load (not stored in config.yaml), so they are rendered explicitly
// rather than from a Config field.
type configPaths struct {
	DBPath   string `yaml:"db_path"`
	CacheDir string `yaml:"cache_dir"`
}

// Group returns the config split into its user-facing profiles. The plain view
// (advanced=false) shows core, local-semantic, and document-extras; the
// advanced-only knobs (vector index/HNSW tuning and compression) are hidden.
// Group(cfg, true) also emits the advanced profile.
//
// Group is a pure, read-only projection of the resolved config: it never writes
// the config file, so using the plain view can never drop or rewrite an
// advanced/unknown knob (the on-disk file is the single source of truth).
func Group(cfg *AppConfig, advanced bool) []ProfileView {
	if cfg == nil {
		return nil
	}
	out := make([]ProfileView, 0, len(profileOrder))
	for _, id := range profileOrder {
		if id == "advanced" && !advanced {
			continue
		}
		pv := ProfileView{ID: id, Title: id, Summary: profileSummary[id]}
		// vector_index/compression live only in the "advanced" profile, which the
		// loop above skips entirely when advanced is false, so advanced-only
		// visibility is handled there — no per-key guard needed here.
		for _, key := range profileSections[id] {
			if block, ok := renderSection(key, cfg.Config); ok {
				pv.Sections = append(pv.Sections, block)
			}
		}
		if id == "core" {
			if block, ok := renderStruct("paths", configPaths{DBPath: cfg.DBPath, CacheDir: cfg.CacheDir}); ok {
				pv.Sections = append(pv.Sections, block)
			}
		}
		out = append(out, pv)
	}
	return out
}

// HasAdvancedKnobs reports whether the config carries any advanced-only knob
// (vector index or compression) that the plain view hides. It lets the caller
// nudge the user toward `seek config --advanced`.
func HasAdvancedKnobs(cfg Config) bool {
	_, vi := renderSection("vector_index", cfg)
	_, comp := renderSection("compression", cfg)
	return vi || comp
}

// renderSection renders one config section as an indented YAML block keyed by
// key. It reports ok=false when the section is the zero value (nothing to show),
// so empty profiles stay quiet. Struct field order is preserved by yaml.v3.
func renderSection(key string, cfg Config) (string, bool) {
	var v any
	switch key {
	case "search":
		v = cfg.Search
	case "filters":
		v = cfg.Filters
	case "aggregations":
		v = cfg.Aggregations
	case "privacy":
		v = cfg.Privacy
	case "chunk":
		v = cfg.Chunk
	case "embedding":
		v = cfg.Embedding
	case "rerank":
		v = cfg.Rerank
	case "semantic":
		v = cfg.Semantic
	case "ocr":
		v = cfg.OCR
	case "extractor":
		v = cfg.Extractor
	case "vector_index":
		v = cfg.VectorIndex
	case "compression":
		v = cfg.Compression
	default:
		return "", false
	}
	return renderStruct(key, v)
}

// renderStruct marshals v and indents it under key so the block reads like the
// config file (two-space indent). An empty struct (v marshals to "{}") is
// reported as not-ok so callers skip it.
func renderStruct(key string, v any) (string, bool) {
	// An encoder with 2-space indent keeps nested blocks (e.g. vector_index.hnsw)
	// aligned with the two-space convention used everywhere else in the output;
	// yaml.Marshal defaults to a four-space nested indent.
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		enc.Close()
		return "", false
	}
	enc.Close()
	body := strings.TrimRight(buf.String(), "\n")
	if body == "{}" {
		return "", false
	}
	lines := strings.Split(body, "\n")
	var out strings.Builder
	out.WriteString(key + ":\n")
	for _, line := range lines {
		if line == "" {
			out.WriteByte('\n')
			continue
		}
		out.WriteString("  ")
		out.WriteString(line)
		out.WriteByte('\n')
	}
	return out.String(), true
}
