package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// bareSemverRE is the canonical version form carried by plugin.json and the
// CHANGELOG headings. The v-prefixed form belongs only on git tags, release
// archive names, and the binary's --version string, so it is rejected here.
var bareSemverRE = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.-]+)?$`)

// releasedHeadingRE captures the version from a released CHANGELOG heading,
// e.g. "## [0.6.1] - 2026-10-09" → "0.6.1". It deliberately does not match
// "## [Unreleased]" (no leading digit).
var releasedHeadingRE = regexp.MustCompile(`^## \[([0-9]+\.[0-9]+\.[0-9]+[^\]]*)\]`)

// TestReleaseVersionConsistency guards against version drift (see AGENTS.md
// "Releasing"). The plugin manifests must always agree with the CHANGELOG:
//
//   - plugin.json's and .claude-plugin/plugin.json's "version" are bare x.y.z
//     and must equal the newest released CHANGELOG heading; `make release` writes
//     all together, so a mismatch means one was hand-edited without the others.
//   - the CHANGELOG must open with exactly one "## [Unreleased]" section. A
//     second/stray section (or a missing reopen after a release) is the exact
//     drift the release script prevents and this guard catches.
func TestReleaseVersionConsistency(t *testing.T) {
	manifestPaths := []string{
		"plugin.json",
		filepath.Join(".claude-plugin", "plugin.json"),
	}

	headings := changelogHeadings(t)
	if len(headings) == 0 {
		t.Fatal("CHANGELOG.md has no '## ' headings")
	}

	if headings[0] != "## [Unreleased]" {
		t.Errorf("CHANGELOG.md must open with '## [Unreleased]', got %q", headings[0])
	}
	if n := countExact(headings, "## [Unreleased]"); n != 1 {
		t.Errorf("CHANGELOG.md must have exactly one '## [Unreleased]' heading, found %d — an orphaned second one means a release was promoted without reopening a fresh section", n)
	}

	topReleased := ""
	for _, h := range headings {
		if m := releasedHeadingRE.FindStringSubmatch(h); m != nil {
			topReleased = m[1]
			break
		}
	}
	if topReleased == "" {
		t.Fatal("CHANGELOG.md has no released '## [x.y.z]' heading to compare against plugin manifests")
	}

	for _, path := range manifestPaths {
		version := readPluginVersion(t, path)
		if !bareSemverRE.MatchString(version) {
			t.Errorf("%s version %q is not a bare x.y.z semver (the v-prefixed form is only for tags/archives/binaries)", path, version)
		}
		if version != topReleased {
			t.Errorf("version drift: %s has %q but the newest CHANGELOG heading is %q — cut releases with `make release VERSION=...` instead of editing one side by hand", path, version, topReleased)
		}
	}
}

// readPluginVersion returns the "version" field of the plugin manifest at path.
func readPluginVersion(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var plugin struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &plugin); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	if plugin.Version == "" {
		t.Fatalf("%s has no \"version\" field", path)
	}
	return plugin.Version
}

// changelogHeadings returns every level-2 heading ("## …") in file order.
func changelogHeadings(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile("CHANGELOG.md")
	if err != nil {
		t.Fatalf("read CHANGELOG.md: %v", err)
	}
	var headings []string
	// Windows checkouts (* text=auto + core.autocrlf=true) materialize
	// CHANGELOG.md with CRLF endings; normalize them so a trailing \r cannot
	// leak into the exact-string heading comparisons below.
	content := strings.ReplaceAll(string(data), "\r\n", "\n")
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "## ") {
			headings = append(headings, line)
		}
	}
	return headings
}

// countExact returns how many entries in ss equal want.
func countExact(ss []string, want string) int {
	n := 0
	for _, s := range ss {
		if s == want {
			n++
		}
	}
	return n
}
