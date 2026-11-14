package cmd

import (
	"fmt"
	"os"

	"github.com/alecthomas/kong"
)

// AdvancedCmd groups the advanced/introspection subcommands under
// `seek advanced`. It is a plain kong parent command; the subcommands reuse
// their existing command structs (SchemaCmd, ParsersCmd, AnalyzeCmd) so the
// Run logic and flags stay in their own cmd files. The legacy top-level
// schema/parsers/analyze commands reach the same structs without the
// "advanced" ancestor, which is how the deprecation hook tells the two paths
// apart.
type AdvancedCmd struct {
	Schema  SchemaCmd  `cmd:"" help:"Show or validate schema"`
	Parsers ParsersCmd `cmd:"" help:"List schema-driven parser definitions"`
	Analyze AnalyzeCmd `cmd:"" help:"Analyze text (tokenize, stem)"`
}

// viaAdvanced reports whether the selected command was reached through the
// `seek advanced` group. kong flattens the group into the top-level help, so
// the only way to distinguish the canonical `seek advanced schema` path from
// the legacy `seek schema` shim is to inspect the command path for the
// "advanced" ancestor.
func viaAdvanced(ctx *kong.Context) bool {
	for _, p := range ctx.Path {
		if p.Command != nil && p.Command.Name == "advanced" {
			return true
		}
	}
	return false
}

// deprecate prints a one-line deprecation warning to stderr when a legacy
// top-level command (schema/parsers/analyze) is invoked directly, pointing the
// user at the canonical `seek advanced` group. It is a no-op on the advanced
// path and on --help (BeforeApply does not run while rendering help). The
// warning goes to stderr only, so --json stdout stays clean.
func deprecate(ctx *kong.Context, name, canonical string) {
	if viaAdvanced(ctx) {
		return
	}
	fmt.Fprintf(os.Stderr, "%s: deprecated; use '%s'\n", name, canonical)
}
