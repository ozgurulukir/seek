package main

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"sort"
	"strings"
	"testing"
)

// archPackage is the subset of `go list -json` output the layering test needs.
type archPackage struct {
	ImportPath   string
	Imports      []string
	TestImports  []string
	XTestImports []string
	Standard     bool
}

// modulePackages runs `go list -json` in dir and returns the non-standard
// packages of this repository (main module or the renameio module).
func modulePackages(t *testing.T, dir string) []archPackage {
	t.Helper()
	cmd := exec.Command("go", "list", "-tags", "fts5 sqlite_fts5", "-json", "./...")
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list in %s failed: %v\n%s", dir, err, stderr.String())
	}
	var pkgs []archPackage
	dec := json.NewDecoder(bytes.NewReader(out))
	for dec.More() {
		var p archPackage
		if err := dec.Decode(&p); err != nil {
			t.Fatalf("decoding go list output: %v", err)
		}
		if p.Standard {
			continue
		}
		pkgs = append(pkgs, p)
	}
	if len(pkgs) == 0 {
		t.Fatalf("go list in %s returned no packages; the guard would verify nothing", dir)
	}
	return pkgs
}

const (
	modulePath   = "github.com/ozgurulukir/seek"
	renameioPath = "github.com/google/renameio"
)

// layer classifies a module package into the architecture layers.
func layer(importPath string) string {
	switch {
	case importPath == modulePath:
		return "root"
	case importPath == modulePath+"/cmd":
		return "cmd"
	case strings.HasPrefix(importPath, modulePath+"/internal/"):
		return "internal"
	default:
		return "third_party"
	}
}

// inModule reports whether importPath belongs to either repository module.
func inModule(importPath string) bool {
	return importPath == modulePath ||
		strings.HasPrefix(importPath, modulePath+"/") ||
		importPath == renameioPath ||
		strings.HasPrefix(importPath, renameioPath+"/")
}

// TestImportGraphRemainsAcyclicAndLayered pins the architecture documented in
// AGENTS.md: root → cmd → internal, with internal never reaching back up and
// no import cycles anywhere. Generic graph tools that match by symbol name
// (code-tandem) report phantom cycles such as internal → cmd; this test checks
// the real import statements instead.
//
// In-package test imports (TestImports) count as package edges, making this
// guard stricter than the Go toolchain: a test-only helper importing the
// package under test is legal to the compiler but still an architecture
// smell here, so it is reported. External test packages (XTest, e.g.
// search_test importing app) are excluded: nothing can import them, so they
// cannot close a cycle — importing app from search_test is the sanctioned
// pattern for exercising the production composition root.
func TestImportGraphRemainsAcyclicAndLayered(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go tool not on PATH; cannot verify import graph")
	}
	pkgs := modulePackages(t, ".")

	edges := map[string][]string{}
	for _, p := range pkgs {
		for _, dep := range append(append([]string{}, p.Imports...), p.TestImports...) {
			if inModule(dep) {
				edges[p.ImportPath] = append(edges[p.ImportPath], dep)
			}
		}
	}

	t.Run("no import cycles", func(t *testing.T) {
		visiting := map[string]bool{}
		visited := map[string]bool{}
		var stack []string
		var reportCycle func(string)
		reportCycle = func(from string) {
			if visited[from] {
				return
			}
			visiting[from] = true
			stack = append(stack, from)
			for _, to := range edges[from] {
				if visiting[to] {
					start := 0
					for stack[start] != to {
						start++
					}
					t.Errorf("import cycle detected: %s", strings.Join(append(append([]string{}, stack[start:]...), to), " -> "))
				} else if !visited[to] {
					reportCycle(to)
				}
			}
			stack = stack[:len(stack)-1]
			visiting[from] = false
			visited[from] = true
		}
		var roots []string
		for p := range edges {
			roots = append(roots, p)
		}
		sort.Strings(roots)
		for _, p := range roots {
			reportCycle(p)
		}
	})

	t.Run("layering directions", func(t *testing.T) {
		for from, deps := range edges {
			for _, to := range deps {
				violation := ""
				switch {
				case layer(from) == "internal" && (layer(to) == "cmd" || layer(to) == "root"):
					violation = "internal must not import cmd or root"
				case layer(from) == "cmd" && layer(to) == "root":
					violation = "cmd must not import root"
				case strings.HasPrefix(from, modulePath) && layer(from) == "third_party":
					violation = "main-module package outside root/cmd/internal must be classified in layer()"
				}
				if violation != "" {
					t.Errorf("%s -> %s: %s", from, to, violation)
				}
			}
		}
	})

	// The renameio module is a vendored leaf: it must never reach back into
	// the main module, keeping it acyclic by construction.
	t.Run("renameio stays a leaf", func(t *testing.T) {
		for _, p := range modulePackages(t, "third_party/renameio") {
			for _, dep := range append(append([]string{}, p.Imports...), p.TestImports...) {
				if strings.HasPrefix(dep, modulePath) {
					t.Errorf("%s -> %s: third_party must not import the main module", p.ImportPath, dep)
				}
			}
		}
	})
}
