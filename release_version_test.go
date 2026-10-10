package main

import (
	"encoding/json"
	"os"
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
// "Releasing"). Two surfaces must always agree:
//
//   - plugin.json's "version" is the bare x.y.z and must equal the newest
//     released CHANGELOG heading; `make release` writes both together, so a
//     mismatch means one was hand-edited without the other.
//   - the CHANGELOG must open with exactly one "## [Unreleased]" section. A
//     second/stray section (or a missing reopen after a release) is the exact
//     drift the release script prevents and this guard catches.
func TestReleaseVersionConsistency(t *testing.T) {
	pluginVersion := readPluginVersion(t)
	if !bareSemverRE.MatchString(pluginVersion) {
		t.Errorf("plugin.json version %q is not a bare x.y.z semver (the v-prefixed form is only for tags/archives/binaries)", pluginVersion)
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
		t.Fatal("CHANGELOG.md has no released '## [x.y.z]' heading to compare against plugin.json")
	}
	if pluginVersion != topReleased {
		t.Errorf("version drift: plugin.json has %q but the newest CHANGELOG heading is %q — cut releases with `make release VERSION=...` instead of editing one side by hand", pluginVersion, topReleased)
	}
}

// readPluginVersion returns the "version" field of plugin.json.
func readPluginVersion(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("plugin.json")
	if err != nil {
		t.Fatalf("read plugin.json: %v", err)
	}
	var plugin struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &plugin); err != nil {
		t.Fatalf("parse plugin.json: %v", err)
	}
	if plugin.Version == "" {
		t.Fatal("plugin.json has no \"version\" field")
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
	for _, line := range strings.Split(string(data), "\n") {
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
