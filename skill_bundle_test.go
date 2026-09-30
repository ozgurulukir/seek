package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// serviceDirs are the optional external-service homes under tools/ (AGENTS.md
// monorepo principle). A new top-level directory under tools/ must be added
// here consciously: the bundle test then walks it and demands every file be
// either listed in the sync script or explicitly excluded below.
var serviceDirs = []string{
	"tools/semantic",
	"tools/xberg_server",
	"tools/flashrank_server",
	"tools/embed_server",
}

// bundleWalkExcluded names are skipped when walking the service trees: the
// local virtualenv, Python bytecode caches, and editor droppings never belong
// to the bundled sources.
var bundleWalkExcluded = map[string]bool{
	".venv":       true,
	"__pycache__": true,
}

// syncScriptPath is the single curated list of canonical → bundled file
// mappings. The test parses it instead of keeping a second copy: one list,
// two consumers (this guard and scripts/sync-skill-services.py).
const syncScriptPath = "scripts/sync-skill-services.py"

var syncPairRE = regexp.MustCompile(`\("([^"]+)",\s*"([^"]+)"\),`)

// normalizeEOL strips \r so a checkout made before the .gitattributes
// eol=lf rule (or an editor that wrote CRLF) cannot raise false drift
// alarms. With the rule in place both sides are LF on disk; this keeps the
// guard correct even without it.
func normalizeEOL(data []byte) []byte {
	return bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
}

// parseSyncPairs extracts the (canonical, bundled) pairs from the sync
// script. Parsing (rather than duplicating) the list keeps the script the
// only place a mapping is declared; a restructure that breaks the regex
// fails here loudly instead of silently disarming the guard.
func parseSyncPairs(t *testing.T) [][2]string {
	t.Helper()
	data, err := os.ReadFile(syncScriptPath)
	if err != nil {
		t.Fatalf("read %s: %v", syncScriptPath, err)
	}
	matches := syncPairRE.FindAllStringSubmatch(string(data), -1)
	if len(matches) == 0 {
		t.Fatalf("no (source, target) pairs parsed from %s; the bundle guard tracks the script's FILES list and must be updated with it", syncScriptPath)
	}
	pairs := make([][2]string, 0, len(matches))
	for _, m := range matches {
		pairs = append(pairs, [2]string{m[1], m[2]})
	}
	return pairs
}

// walkServiceFiles returns the slash-relative paths of all regular files
// under dir, skipping excluded directory names and bytecode droppings.
func walkServiceFiles(t *testing.T, dir string) []string {
	t.Helper()
	var files []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		name := info.Name()
		if info.IsDir() {
			if bundleWalkExcluded[name] {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(name, ".pyc") {
			return nil
		}
		files = append(files, filepath.ToSlash(path))
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return files
}

// TestSkillServiceBundleIsSynchronized pins the skill-bundle SSOT contract:
// tools/ owns the optional service sources, skills/seek/scripts/services/
// holds generated copies for the standalone skill, and
// scripts/sync-skill-services.py (make skill-services) is the only writer of
// that mirror. Drift — an edited tool, a hand-edited bundle copy, or an
// unlisted new file — fails here and in CI (skill-services.yml), so a fix
// never has to be applied twice.
func TestSkillServiceBundleIsSynchronized(t *testing.T) {
	pairs := parseSyncPairs(t)

	sources := make(map[string]bool, len(pairs))
	targets := make(map[string]bool, len(pairs))
	for _, pair := range pairs {
		sources[pair[0]] = true
		targets[pair[1]] = true
	}

	var problems []string

	// Every declared pair must exist and be byte-identical.
	for _, pair := range pairs {
		source, target := pair[0], pair[1]
		sourceData, err := os.ReadFile(source)
		if err != nil {
			problems = append(problems, "read canonical "+source+": "+err.Error())
			continue
		}
		targetData, err := os.ReadFile(target)
		if err != nil {
			problems = append(problems, "read bundled "+target+": "+err.Error())
			continue
		}
		if !bytes.Equal(normalizeEOL(sourceData), normalizeEOL(targetData)) {
			problems = append(problems, "stale bundled copy: "+target+" differs from "+source+" — run: make skill-services")
		}
	}

	// No undeclared directory may appear under tools/: the walk below can
	// only enforce what the serviceDirs list names.
	toolsEntries, err := os.ReadDir("tools")
	if err != nil {
		t.Fatalf("read tools/: %v", err)
	}
	known := make(map[string]bool, len(serviceDirs))
	for _, dir := range serviceDirs {
		known[filepath.ToSlash(dir)] = true
	}
	for _, entry := range toolsEntries {
		if !entry.IsDir() {
			continue
		}
		rel := "tools/" + entry.Name()
		if !known[rel] {
			problems = append(problems, "undeclared tools/ directory: "+rel+" — add it to serviceDirs (and the sync script) or keep it out of tools/")
		}
	}

	// Every source file in a service tree must be listed for bundling:
	// a canonical file that silently lacks a bundled copy recreates the
	// hand-maintained-twin problem one file at a time.
	for _, dir := range serviceDirs {
		for _, file := range walkServiceFiles(t, dir) {
			if !sources[file] {
				problems = append(problems, "unlisted canonical file: "+file+" — add it to scripts/sync-skill-services.py or exclude it deliberately")
			}
		}
	}

	// Every bundled file must have a canonical source: hand-edits or
	// hand-additions in the mirror are exactly how the two trees drift.
	for _, file := range walkServiceFiles(t, "skills/seek/scripts/services") {
		if !targets[file] {
			problems = append(problems, "bundled file without canonical source: "+file+" — edit its tools/ counterpart and run: make skill-services")
		}
	}

	sort.Strings(problems)
	for _, p := range problems {
		t.Error(p)
	}
}
