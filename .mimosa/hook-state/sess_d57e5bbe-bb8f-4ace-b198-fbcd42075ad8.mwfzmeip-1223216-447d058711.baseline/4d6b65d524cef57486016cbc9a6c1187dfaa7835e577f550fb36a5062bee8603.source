package cmd

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/ozgurulukir/seek/internal/config"
)

// testGrammar is the minimal command surface needed to exercise the
// advanced/schema/parsers/analyze deprecation contract. It mirrors the real
// main.go wiring without pulling in the full CLI.
type testGrammar struct {
	Advanced AdvancedCmd `cmd:"" help:"Advanced commands"`
	Schema   SchemaCmd   `cmd:"" help:"Show or validate schema"`
	Parsers  ParsersCmd  `cmd:"" help:"Manage schema-driven parser definitions"`
	Analyze  AnalyzeCmd  `cmd:"" help:"Analyze text"`
}

// noopExit is a kong Exit hook that does nothing, so --help (which calls
// Exit(0) after rendering help) does not terminate the test process.
var noopExit = func(int) {}

// errKongExit is the sentinel panic used to simulate kong's real Exit(0) —
// which terminates the process before BeforeApply — inside a test.
var errKongExit = errors.New("kong-exit")

// newTestApp builds a kong app over testGrammar and parses args.
func newTestApp(t *testing.T, exitFn func(int), args ...string) *kong.Context {
	t.Helper()
	var g testGrammar
	k := kong.Must(&g, kong.Name("seek"), kong.Exit(exitFn))
	ctx, err := k.Parse(args)
	if err != nil {
		t.Fatalf("kong.Parse(%v) = %v", args, err)
	}
	return ctx
}

// runApp parses args and runs the selected command, returning any error.
func runApp(t *testing.T, cfg *config.AppConfig, args ...string) error {
	t.Helper()
	return newTestApp(t, noopExit, args...).Run(cfg)
}

// captureStd swaps os.Stdout/os.Stderr for pipes while fn runs, returning the
// captured (stdout, stderr). deprecate writes directly to os.Stderr (not to
// kong's writer), so capturing the real os streams is required.
func captureStd(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()
	origOut, origErr := os.Stdout, os.Stderr
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = wOut, wErr
	defer func() { os.Stdout, os.Stderr = origOut, origErr }()

	var outBuf, errBuf bytes.Buffer
	done := make(chan struct{})
	go func() {
		io.Copy(&outBuf, rOut)
		io.Copy(&errBuf, rErr)
		close(done)
	}()

	fn()

	wOut.Close()
	wErr.Close()
	<-done
	return outBuf.String(), errBuf.String()
}

// captureExit runs fn (which triggers kong's Exit via a sentinel panic),
// capturing stdout/stderr, and reports whether the sentinel Exit fired. Closing
// the pipes in the deferred unwind lets the reader goroutine finish before the
// test continues.
func captureExit(t *testing.T, sentinel error, fn func()) (stdout, stderr string, exited bool) {
	t.Helper()
	origOut, origErr := os.Stdout, os.Stderr
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = wOut, wErr

	var outBuf, errBuf bytes.Buffer
	done := make(chan struct{})
	go func() {
		io.Copy(&outBuf, rOut)
		io.Copy(&errBuf, rErr)
		close(done)
	}()

	func() {
		defer func() {
			wOut.Close()
			wErr.Close()
			if r := recover(); r != nil {
				if r == sentinel {
					exited = true
				} else {
					panic(r)
				}
			}
		}()
		fn()
	}()

	<-done
	os.Stdout, os.Stderr = origOut, origErr
	return outBuf.String(), errBuf.String(), exited
}

// TestViaAdvanced pins the path inspection that distinguishes the canonical
// `advanced` path from the legacy top-level shim.
func TestViaAdvanced(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{"legacy schema", []string{"schema"}, false},
		{"advanced schema", []string{"advanced", "schema"}, true},
		{"legacy parsers list", []string{"parsers", "list"}, false},
		{"advanced parsers list", []string{"advanced", "parsers", "list"}, true},
		{"legacy analyze", []string{"analyze", "x"}, false},
		{"advanced analyze", []string{"advanced", "analyze", "x"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := viaAdvanced(newTestApp(t, noopExit, tc.args...)); got != tc.want {
				t.Errorf("viaAdvanced(%v) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}

// TestDeprecate_RoutesToStderrOnly verifies the helper writes the one-line
// warning to stderr only, and stays silent on the advanced path.
func TestDeprecate_RoutesToStderrOnly(t *testing.T) {
	const canonical = "schema: deprecated; use 'seek advanced schema'"

	// Legacy path emits the warning to stderr, nothing to stdout.
	stdCtx := newTestApp(t, noopExit, "schema")
	stdout, stderr := captureStd(t, func() { deprecate(stdCtx, "schema", "seek advanced schema") })
	if !strings.Contains(stderr, canonical) {
		t.Errorf("legacy deprecate stderr = %q, want it to contain %q", stderr, canonical)
	}
	if strings.Contains(stdout, "deprecated") {
		t.Errorf("legacy deprecate stdout must be clean, got %q", stdout)
	}

	// Advanced path is silent on both streams.
	advCtx := newTestApp(t, noopExit, "advanced", "schema")
	stdout, stderr = captureStd(t, func() { deprecate(advCtx, "schema", "seek advanced schema") })
	if strings.Contains(stderr, "deprecated") {
		t.Errorf("advanced deprecate stderr must be silent, got %q", stderr)
	}
	_ = stdout
}

// TestDeprecation_Contract_AllCommands is the end-to-end proof: through the real
// kong app, legacy top-level commands print the deprecation to stderr only with
// clean stdout, while the advanced path is silent. schema/parsers/analyze have
// no --json flag, so the stderr-only contract holds for all their output.
func TestDeprecation_Contract_AllCommands(t *testing.T) {
	cfg := &config.AppConfig{}

	cases := []struct {
		name       string
		legacyArgs []string
		advArgs    []string
		deprLine   string
		stdoutWant string
	}{
		{
			name:       "schema",
			legacyArgs: []string{"schema", "--show"},
			advArgs:    []string{"advanced", "schema", "--show"},
			deprLine:   "schema: deprecated; use 'seek advanced schema'",
			stdoutWant: "{", // JSON payload
		},
		{
			name:       "parsers",
			legacyArgs: []string{"parsers", "list"},
			advArgs:    []string{"advanced", "parsers", "list"},
			deprLine:   "parsers: deprecated; use 'seek advanced parsers list'",
			stdoutWant: "SCHEMA", // table header
		},
		{
			name:       "analyze",
			legacyArgs: []string{"analyze", "hello world"},
			advArgs:    []string{"advanced", "analyze", "hello world"},
			deprLine:   "analyze: deprecated; use 'seek advanced analyze'",
			stdoutWant: "Analyzed",
		},
	}

	for _, tc := range cases {
		t.Run("legacy_"+tc.name, func(t *testing.T) {
			stdout, stderr := captureStd(t, func() {
				if err := runApp(t, cfg, tc.legacyArgs...); err != nil {
					t.Fatalf("Run %v: %v", tc.legacyArgs, err)
				}
			})
			if !strings.Contains(stderr, tc.deprLine) {
				t.Errorf("legacy stderr missing %q; got %q", tc.deprLine, stderr)
			}
			// stdout carries the real output, never the deprecation.
			if strings.Contains(stdout, "deprecated") {
				t.Errorf("legacy stdout must be clean of deprecation; got %q", stdout)
			}
			if !strings.Contains(stdout, tc.stdoutWant) {
				t.Errorf("legacy stdout missing %q; got %q", tc.stdoutWant, stdout)
			}
			// The command's own output must not leak onto stderr.
			if strings.Contains(stderr, tc.stdoutWant) {
				t.Errorf("legacy stderr leaked command output %q; got %q", tc.stdoutWant, stderr)
			}
		})

		t.Run("advanced_"+tc.name, func(t *testing.T) {
			stdout, stderr := captureStd(t, func() {
				if err := runApp(t, cfg, tc.advArgs...); err != nil {
					t.Fatalf("Run %v: %v", tc.advArgs, err)
				}
			})
			if strings.Contains(stderr, "deprecated") {
				t.Errorf("advanced stderr must be silent; got %q", stderr)
			}
			if !strings.Contains(stdout, tc.stdoutWant) {
				t.Errorf("advanced stdout missing %q; got %q", tc.stdoutWant, stdout)
			}
		})
	}
}

// TestDeprecation_ExitCodeParity verifies the legacy and advanced paths run the
// same Run logic, so they return identical errors (hence identical exit codes).
func TestDeprecation_ExitCodeParity(t *testing.T) {
	cfg := &config.AppConfig{}
	cases := []struct {
		name       string
		legacyArgs []string
		advArgs    []string
	}{
		{"schema (no subcommand)", []string{"schema"}, []string{"advanced", "schema"}},
		{"analyze", []string{"analyze", "x"}, []string{"advanced", "analyze", "x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			legacyErr := runApp(t, cfg, tc.legacyArgs...)
			advErr := runApp(t, cfg, tc.advArgs...)
			if (legacyErr == nil) != (advErr == nil) {
				t.Errorf("exit-code parity: legacy=%v advanced=%v",
					legacyErr == nil, advErr == nil)
			}
			if legacyErr != nil && advErr != nil && legacyErr.Error() != advErr.Error() {
				t.Errorf("exit-code parity: errors differ:\n legacy=%q\n advanced=%q",
					legacyErr, advErr)
			}
		})
	}
}

// TestDeprecation_HelpDoesNotFire verifies --help renders help without firing
// the BeforeApply deprecation hook. In production --help's BeforeReset calls
// Exit(0), which terminates before BeforeApply; the sentinel panic simulates
// that termination so the hook's absence is observable. kong renders help to
// its own writer (kong.Writers); the deprecate path writes to os.Stderr, which
// is captured via a pipe swap.
func TestDeprecation_HelpDoesNotFire(t *testing.T) {
	var g testGrammar
	var helpOut, helpErr bytes.Buffer
	k := kong.Must(&g,
		kong.Name("seek"),
		kong.Writers(&helpOut, &helpErr),
		kong.Exit(func(int) { panic(errKongExit) }),
	)

	// deprecate writes to os.Stderr; capture it via a pipe swap.
	origErr := os.Stderr
	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = wErr
	defer func() { os.Stderr = origErr }()

	var depErr bytes.Buffer
	done := make(chan struct{})
	go func() { io.Copy(&depErr, rErr); close(done) }()

	exited := false
	func() {
		defer func() {
			wErr.Close()
			if r := recover(); r == errKongExit {
				exited = true
			} else {
				panic(r)
			}
		}()
		if _, err := k.Parse([]string{"schema", "--help"}); err != nil {
			t.Errorf("kong.Parse(schema --help): %v", err)
		}
	}()
	<-done

	if !exited {
		t.Fatalf("expected kong Exit(0) to fire on --help (simulated via panic)")
	}
	if helpOut.String() == "" {
		t.Errorf("schema --help expected help text on stdout")
	}
	if strings.Contains(depErr.String(), "deprecated") {
		t.Errorf("schema --help stderr must not contain deprecation; got %q", depErr.String())
	}
}
