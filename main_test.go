package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/alecthomas/kong"
)

// exitSignal is the sentinel a kong Exit callback panics with to fake "exit"
// after printing help, mirroring kong's own help tests. It lets the render
// stop at the help print instead of falling through to command validation
// (which would fail because --help selects no command).
type exitSignal struct{}

// updateGolden, when set via `go test -update`, rewrites the golden help
// snapshots in testdata/help instead of comparing. C3/C4/C5 change help text,
// so the snapshots are regenerated with:
//
//	go test -tags "fts5 sqlite_fts5" -update .
var updateGolden = flag.Bool("update", false, "update golden help snapshots in testdata/help")

// goldenPath returns the on-disk location of a golden help snapshot.
func goldenPath(name string) string {
	return filepath.Join("testdata", "help", name+".txt")
}

// newHelpApp builds a kong app from the real grammar (the same struct main.go
// parses), rendering to an in-memory buffer and neutralizing Exit so the test
// can capture help without terminating the process. The options mirror main.go
// (Name, Description, Vars) so the rendered help is byte-identical to the
// shipped binary. A fresh copy of the grammar is used each call so repeated
// renders cannot contaminate one another.
//
// kong.Exit panics with exitSignal after printing help; the recover below
// treats that as the expected "printed help, stop" outcome and surfaces any
// other error (a genuine parse failure) as a test failure.
func newHelpApp(t *testing.T, args ...string) (string) {
	t.Helper()
	grammar := cli // value copy: nested command structs are copied by value
	var buf bytes.Buffer
	app, err := kong.New(&grammar,
		kong.Name("seek"),
		kong.Description("Personal document search engine — BM25 + vector hybrid search"),
		kong.Writers(&buf, &buf),
		kong.Exit(func(int) { panic(exitSignal{}) }),
		kong.Vars{"version": fmt.Sprintf("%s (%s)", Version, Commit)},
	)
	if err != nil {
		t.Fatalf("kong.New: %v", err)
	}
	var parseErr error
	exited := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				if _, ok := r.(exitSignal); !ok {
					panic(r) // unexpected panic: re-raise
				}
				exited = true
			}
		}()
		_, parseErr = app.Parse(args)
	}()
	if !exited {
		// No help exit: Parse returned normally, so any non-nil error is a
		// genuine parse failure worth surfacing.
		if parseErr != nil {
			t.Fatalf("Parse(%v): %v", args, parseErr)
		}
		t.Fatalf("Parse(%v): help was not printed (Exit did not fire)", args)
	}
	return buf.String()
}

// goldenHelp renders help for the given kong args and either writes the
// snapshot (when -update) or compares against the committed golden file.
func goldenHelp(t *testing.T, name string, args ...string) {
	t.Helper()
	got := newHelpApp(t, args...)

	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(goldenPath(name)), 0o755); err != nil {
			t.Fatalf("mkdir golden dir: %v", err)
		}
		if err := os.WriteFile(goldenPath(name), []byte(got), 0o644); err != nil {
			t.Fatalf("write golden %s: %v", name, err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath(name))
	if err != nil {
		t.Fatalf("read golden %s: %v (regenerate with go test -update .)", name, err)
	}
	if string(want) != got {
		t.Errorf("golden help %s mismatch\n--- got ---\n%s\n--- want ---\n%s", name, got, string(want))
	}
}

// TestHelpGolden_TopLevel pins the top-level `seek --help` surface. It is the
// single source of truth for which top-level commands exist and how each is
// described; adding or removing a command here fails the snapshot.
func TestHelpGolden_TopLevel(t *testing.T) {
	goldenHelp(t, "top-level", "--help")
}

// TestHelpGolden_Add pins `seek add --help`.
func TestHelpGolden_Add(t *testing.T) {
	goldenHelp(t, "add", "add", "--help")
}

// TestHelpGolden_Search pins `seek search --help`.
func TestHelpGolden_Search(t *testing.T) {
	goldenHelp(t, "search", "search", "--help")
}

// TestHelpGolden_Collection pins `seek collection --help`.
func TestHelpGolden_Collection(t *testing.T) {
	goldenHelp(t, "collection", "collection", "--help")
}

// TestHelpGolden_Advanced pins `seek advanced --help`, the group that owns the
// canonical schema/parsers/analyze subcommands.
func TestHelpGolden_Advanced(t *testing.T) {
	goldenHelp(t, "advanced", "advanced", "--help")
}
