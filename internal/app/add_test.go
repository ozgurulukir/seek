package app

import (
	"strings"
	"testing"

	"github.com/ozgurulukir/seek/internal/store"
)

// wantRequest is the expected outcome of BuildAddRequest for a flag set. When
// errSub is set, the call is expected to error with a message containing it.
type wantRequest struct {
	typ     store.CollectionType
	agent   string
	parser  string
	pattern string
	backend string
	name    string
	errSub  string
}

// TestBuildAddRequest_Combinations maps every supported add selector to its
// expected typed request. It is the exhaustive "what does this flag produce"
// table for the canonical and legacy selectors.
func TestBuildAddRequest_Combinations(t *testing.T) {
	cases := []struct {
		name  string
		flags AddFlags
		want  wantRequest
	}{
		// implicit default / canonical --type
		{"no flags (implicit markdown)", AddFlags{}, wantRequest{typ: store.CollectionTypeMarkdown, pattern: "**/*.md"}},
		{"--type markdown", AddFlags{Type: "markdown"}, wantRequest{typ: store.CollectionTypeMarkdown, pattern: "**/*.md"}},
		{"--type code", AddFlags{Type: "code"}, wantRequest{typ: store.CollectionTypeCode, pattern: "**/*"}},
		{"--type documents", AddFlags{Type: "documents"}, wantRequest{typ: store.CollectionTypeDocuments, pattern: "**/*"}},
		{"--type pdf", AddFlags{Type: "pdf"}, wantRequest{typ: store.CollectionTypePDF, pattern: "**/*.pdf"}},
		{"--type images", AddFlags{Type: "images"}, wantRequest{typ: store.CollectionTypeImages, pattern: "**/*.{png,jpg,jpeg,webp}"}},

		// native selectors
		{"--claude", AddFlags{Claude: true}, wantRequest{typ: store.CollectionTypeClaude, agent: "claude", name: "claude-conversations"}},
		{"--codex", AddFlags{Codex: true}, wantRequest{typ: store.CollectionTypeCodex, agent: "codex", name: "codex-conversations"}},
		{"--images", AddFlags{Images: true}, wantRequest{typ: store.CollectionTypeImages, pattern: "**/*.{png,jpg,jpeg,webp}"}},
		{"--pdf", AddFlags{Pdf: true}, wantRequest{typ: store.CollectionTypePDF, pattern: "**/*.pdf"}},
		{"--code", AddFlags{Code: true}, wantRequest{typ: store.CollectionTypeCode, pattern: "**/*"}},
		{"--documents", AddFlags{Documents: true}, wantRequest{typ: store.CollectionTypeDocuments, pattern: "**/*"}},
		{"--docs", AddFlags{Docs: true}, wantRequest{typ: store.CollectionTypeDocuments, pattern: "**/*"}},

		// --agent (native and parser)
		{"--agent claude", AddFlags{Agent: "claude"}, wantRequest{typ: store.CollectionTypeClaude, agent: "claude", name: "claude-conversations"}},
		{"--agent codex", AddFlags{Agent: "codex"}, wantRequest{typ: store.CollectionTypeCodex, agent: "codex", name: "codex-conversations"}},
		{"--agent opencode", AddFlags{Agent: "opencode"}, wantRequest{typ: store.CollectionTypeParser, agent: "opencode", parser: "opencode", name: "opencode-conversations"}},
		{"--agent copilot", AddFlags{Agent: "copilot"}, wantRequest{typ: store.CollectionTypeParser, agent: "copilot", parser: "copilot-cli", name: "copilot-cli-conversations"}},
		{"--agent zed", AddFlags{Agent: "zed"}, wantRequest{typ: store.CollectionTypeParser, agent: "zed", parser: "zed", name: "zed-conversations"}},
		{"--agent hermes", AddFlags{Agent: "hermes"}, wantRequest{typ: store.CollectionTypeParser, agent: "hermes", parser: "hermes", name: "hermes-conversations"}},

		// parser selectors (native schema variants included)
		{"--parser opencode", AddFlags{Parser: "opencode"}, wantRequest{typ: store.CollectionTypeParser, agent: "opencode", parser: "opencode", name: "opencode-conversations"}},
		{"--parser copilot-cli", AddFlags{Parser: "copilot-cli"}, wantRequest{typ: store.CollectionTypeParser, agent: "copilot", parser: "copilot-cli", name: "copilot-cli-conversations"}},
		{"--parser zed", AddFlags{Parser: "zed"}, wantRequest{typ: store.CollectionTypeParser, agent: "zed", parser: "zed", name: "zed-conversations"}},
		{"--parser hermes", AddFlags{Parser: "hermes"}, wantRequest{typ: store.CollectionTypeParser, agent: "hermes", parser: "hermes", name: "hermes-conversations"}},
		{"--parser claude", AddFlags{Parser: "claude"}, wantRequest{typ: store.CollectionTypeParser, agent: "claude", parser: "claude", name: "claude-conversations"}},
		{"--claude-schema", AddFlags{ClaudeSchema: true}, wantRequest{typ: store.CollectionTypeParser, agent: "claude", parser: "claude", name: "claude-conversations"}},
		{"--codex-schema", AddFlags{CodexSchema: true}, wantRequest{typ: store.CollectionTypeParser, agent: "codex", parser: "codex", name: "codex-conversations"}},
		{"--opencode", AddFlags{Opencode: true}, wantRequest{typ: store.CollectionTypeParser, agent: "opencode", parser: "opencode", name: "opencode-conversations"}},
		{"--copilot", AddFlags{Copilot: true}, wantRequest{typ: store.CollectionTypeParser, agent: "copilot", parser: "copilot-cli", name: "copilot-cli-conversations"}},
		{"--zed", AddFlags{Zed: true}, wantRequest{typ: store.CollectionTypeParser, agent: "zed", parser: "zed", name: "zed-conversations"}},
		{"--hermes", AddFlags{Hermes: true}, wantRequest{typ: store.CollectionTypeParser, agent: "hermes", parser: "hermes", name: "hermes-conversations"}},

		// name + backend + pattern passthrough
		{"name", AddFlags{Name: "my-col"}, wantRequest{typ: store.CollectionTypeMarkdown, pattern: "**/*.md", name: "my-col"}},
		{"backend", AddFlags{Documents: true, Backend: "xberg"}, wantRequest{typ: store.CollectionTypeDocuments, pattern: "**/*", backend: "xberg"}},
		{"code with -p", AddFlags{Code: true, Pattern: "**/*.go"}, wantRequest{typ: store.CollectionTypeCode, pattern: "**/*.go"}},
		{"markdown with -p", AddFlags{Type: "markdown", Pattern: "notes/*.md"}, wantRequest{typ: store.CollectionTypeMarkdown, pattern: "notes/*.md"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := BuildAddRequest(tc.flags)
			if tc.want.errSub != "" {
				t.Fatalf("unexpected success: %v", req)
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			want := tc.want
			if req.Type != want.typ || req.Agent != want.agent || req.Parser != want.parser ||
				req.Pattern != want.pattern || req.Backend != want.backend || req.Name != want.name {
				t.Errorf("mismatch:\n got  %+v\n want type=%q agent=%q parser=%q pattern=%q backend=%q name=%q",
					req, want.typ, want.agent, want.parser, want.pattern, want.backend, want.name)
			}
		})
	}
}

// TestBuildAddRequest_Conflicts verifies that conflicting selectors return a
// clear error with the right category (native kind clash vs. parser clash).
func TestBuildAddRequest_Conflicts(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name   string
		flags  AddFlags
		errSub string
	}{
		// native kind clashes
		{"--code --pdf", AddFlags{Path: dir, Code: true, Pdf: true}, "conflicting collection types"},
		{"--claude --codex", AddFlags{Path: dir, Claude: true, Codex: true}, "conflicting collection types"},
		{"--type code --pdf", AddFlags{Path: dir, Type: "code", Pdf: true}, "conflicting collection types"},
		{"--agent claude --type code", AddFlags{Path: dir, Agent: "claude", Type: "code"}, "conflicting collection types"},
		{"--type markdown --claude", AddFlags{Path: dir, Type: "markdown", Claude: true}, "conflicting collection types"},
		{"--claude --parser foo", AddFlags{Path: dir, Claude: true, Parser: "foo"}, "conflicting collection types"},
		{"--code --opencode", AddFlags{Path: dir, Code: true, Opencode: true}, "conflicting collection types"},
		{"--claude --codex-schema", AddFlags{Path: dir, Claude: true, CodexSchema: true}, "conflicting collection types"},
		{"--documents --code", AddFlags{Path: dir, Documents: true, Code: true}, "conflicting collection types"},
		// parser clashes (multiple parser sources)
		{"--opencode --copilot", AddFlags{Path: dir, Opencode: true, Copilot: true}, "multiple parser sources"},
		{"--claude-schema --codex-schema", AddFlags{Path: dir, ClaudeSchema: true, CodexSchema: true}, "multiple parser sources"},
		{"--opencode --parser foo", AddFlags{Path: dir, Opencode: true, Parser: "foo"}, "multiple parser sources"},
		{"--zed --hermes", AddFlags{Path: dir, Zed: true, Hermes: true}, "multiple parser sources"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := BuildAddRequest(tc.flags)
			if err == nil {
				t.Fatalf("expected error containing %q, got success", tc.errSub)
			}
			if !strings.Contains(err.Error(), tc.errSub) {
				t.Errorf("expected error containing %q, got: %v", tc.errSub, err)
			}
		})
	}
}

// TestBuildAddRequest_AliasesAccepted verifies that aliases of the same kind do
// NOT conflict.
func TestBuildAddRequest_AliasesAccepted(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name  string
		flags AddFlags
		want  wantRequest
	}{
		{"--documents --docs", AddFlags{Path: dir, Documents: true, Docs: true}, wantRequest{typ: store.CollectionTypeDocuments, pattern: "**/*"}},
		{"--type documents --documents", AddFlags{Path: dir, Type: "documents", Documents: true}, wantRequest{typ: store.CollectionTypeDocuments, pattern: "**/*"}},
		{"--type documents --docs", AddFlags{Path: dir, Type: "documents", Docs: true}, wantRequest{typ: store.CollectionTypeDocuments, pattern: "**/*"}},
		{"--code --type code", AddFlags{Path: dir, Code: true, Type: "code"}, wantRequest{typ: store.CollectionTypeCode, pattern: "**/*"}},
		{"--pdf --type pdf", AddFlags{Path: dir, Pdf: true, Type: "pdf"}, wantRequest{typ: store.CollectionTypePDF, pattern: "**/*.pdf"}},
		{"--claude-schema --parser claude", AddFlags{Path: dir, ClaudeSchema: true, Parser: "claude"}, wantRequest{typ: store.CollectionTypeParser, parser: "claude", agent: "claude"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := BuildAddRequest(tc.flags)
			if err != nil {
				t.Fatalf("alias pair should not conflict: %v", err)
			}
			want := tc.want
			if req.Type != want.typ || req.Parser != want.parser || req.Agent != want.agent || req.Pattern != want.pattern {
				t.Errorf("mismatch: got type=%q parser=%q agent=%q pattern=%q", req.Type, req.Parser, req.Agent, req.Pattern)
			}
		})
	}
}

// TestBuildAddRequest_Parity verifies legacy aliases produce the same typed
// request as their canonical new-syntax counterpart.
func TestBuildAddRequest_Parity(t *testing.T) {
	dir := t.TempDir()
	type pair struct{ old, new string }
	pairs := []pair{
		{"--documents", "--type documents"},
		{"--docs", "--type documents"},
		{"--opencode", "--agent opencode"},
		{"--copilot", "--agent copilot"},
		{"--zed", "--agent zed"},
		{"--hermes", "--agent hermes"},
		{"--claude-schema", "--parser claude"},
		{"--codex-schema", "--parser codex"},
	}
	for _, p := range pairs {
		t.Run(p.old+" == "+p.new, func(t *testing.T) {
			oldReq, err := BuildAddRequest(flagsFromString(p.old, dir))
			if err != nil {
				t.Fatalf("old %q: %v", p.old, err)
			}
			newReq, err := BuildAddRequest(flagsFromString(p.new, dir))
			if err != nil {
				t.Fatalf("new %q: %v", p.new, err)
			}
			if oldReq != newReq {
				t.Errorf("parity mismatch:\n old %+v\n new %+v", oldReq, newReq)
			}
		})
	}
}

// TestBuildAddRequest_Invalid verifies invalid --type/--agent/--backend values
// are rejected before any collection is selected.
func TestBuildAddRequest_Invalid(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name  string
		flags AddFlags
	}{
		{"invalid --type", AddFlags{Path: dir, Type: "video"}},
		{"invalid --agent", AddFlags{Path: dir, Agent: "gemini"}},
		{"invalid --backend", AddFlags{Path: dir, Documents: true, Backend: "foo"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := BuildAddRequest(tc.flags); err == nil {
				t.Fatal("expected error for invalid value, got success")
			}
		})
	}
}

// flagsFromString parses a "--a [--val] --b ..." style case name into AddFlags,
// understanding the selectors exercised by the parity table.
func flagsFromString(name, dir string) AddFlags {
	flags := AddFlags{Path: dir}
	toks := strings.Split(name, " ")
	for i := 0; i < len(toks); i++ {
		tok := toks[i]
		switch tok {
		case "--documents":
			flags.Documents = true
		case "--docs":
			flags.Docs = true
		case "--opencode":
			flags.Opencode = true
		case "--copilot":
			flags.Copilot = true
		case "--zed":
			flags.Zed = true
		case "--hermes":
			flags.Hermes = true
		case "--claude-schema":
			flags.ClaudeSchema = true
		case "--codex-schema":
			flags.CodexSchema = true
		case "--type":
			if i+1 < len(toks) {
				flags.Type = toks[i+1]
				i++
			}
		case "--agent":
			if i+1 < len(toks) {
				flags.Agent = toks[i+1]
				i++
			}
		case "--parser":
			if i+1 < len(toks) {
				flags.Parser = toks[i+1]
				i++
			}
		}
	}
	return flags
}
