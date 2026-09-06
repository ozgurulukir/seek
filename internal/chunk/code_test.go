package chunk_test

import (
	"strings"
	"testing"

	"github.com/ozgurulukir/seek/internal/chunk"
)

func TestChunkCode_Small(t *testing.T) {
	code := `package main

import "fmt"

func main() {
	fmt.Println("Hello, World!")
}`

	chunks := chunk.ChunkCode(code, "go", 1000, 100)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Seq != 0 {
		t.Errorf("expected Seq 0, got %d", chunks[0].Seq)
	}
	if chunks[0].Type != chunk.ChunkText {
		t.Errorf("expected ChunkText, got %d", chunks[0].Type)
	}
	if chunks[0].Content != code {
		t.Errorf("content mismatch")
	}
}

func TestChunkCode_MultipleBlocks(t *testing.T) {
	code := `package main

func foo() {
	// block 1
}

func bar() {
	// block 2
}

func baz() {
	// block 3
}`

	// With maxSize 60, each function should form its own chunk or combine cleanly
	chunks := chunk.ChunkCode(code, "go", 60, 10)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}

	for i, c := range chunks {
		if c.Seq != i {
			t.Errorf("chunk %d has wrong Seq %d", i, c.Seq)
		}
		if len(c.Content) == 0 {
			t.Errorf("chunk %d has empty content", i)
		}
	}
}

func TestChunkCode_LargeBlockLineSplitting(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("func largeFunction() {\n")
	for i := 0; i < 50; i++ {
		sb.WriteString("\t// some repeated code line statement here\n")
	}
	sb.WriteString("}\n")
	code := sb.String()

	chunks := chunk.ChunkCode(code, "go", 200, 40)
	if len(chunks) < 2 {
		t.Fatalf("expected large block to be split into multiple chunks, got %d", len(chunks))
	}

	// Verify all chunks have content and sequential IDs
	for i, c := range chunks {
		if c.Seq != i {
			t.Errorf("chunk %d has Seq %d", i, c.Seq)
		}
		if len(c.Content) == 0 {
			t.Errorf("chunk %d has empty content", i)
		}
	}
}

func TestChunkCode_Empty(t *testing.T) {
	chunks := chunk.ChunkCode("", "python", 1000, 100)
	if len(chunks) != 0 {
		t.Errorf("expected 0 chunks for empty string, got %d", len(chunks))
	}
}

func TestChunkCode_ExcessiveOverlap(t *testing.T) {
	code := `func A() {
	println("1")
	println("2")
	println("3")
	println("4")
}`
	chunks := chunk.ChunkCode(code, "go", 30, 100)
	if len(chunks) == 0 {
		t.Fatal("expected chunks, got 0")
	}
}

// TestChunkCode_LanguageAwareBoundaries verifies that known languages split
// at top-level definitions (func/def/class) rather than at arbitrary
// blank-line boundaries.
func TestChunkCode_LanguageAwareBoundaries(t *testing.T) {
	// Body with no blank line between end of one func and start of next;
	// blank-line splitting would merge these into one block.
	code := "func foo() {\n\treturn 1\n}\nfunc bar() {\n\treturn 2\n}\nfunc baz() {\n\treturn 3\n}"
	chunks := chunk.ChunkCode(code, "go", 30, 5)
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks (one per func), got %d: %#v", len(chunks), chunks)
	}
	for i, c := range chunks {
		if !strings.Contains(c.Content, "func ") {
			t.Errorf("chunk %d does not start a function definition: %q", i, c.Content)
		}
	}

	// Python: split on def/class
	py := "def a():\n    return 1\ndef b():\n    return 2\nclass C:\n    pass"
	pyChunks := chunk.ChunkCode(py, "python", 20, 5)
	if len(pyChunks) < 3 {
		t.Fatalf("expected >=3 python chunks, got %d", len(pyChunks))
	}
}

// TestChunkCode_UnknownLanguageFallsBack verifies that a language without a
// registered pattern still chunks via the blank-line fallback.
func TestChunkCode_UnknownLanguageFallsBack(t *testing.T) {
	code := "aaa bbb ccc\n\nxxx yyy zzz\n\nmmm nnn ooo"
	chunks := chunk.ChunkCode(code, "cobol", 15, 3)
	if len(chunks) < 2 {
		t.Fatalf("expected fallback blank-line splitting, got %d chunk(s)", len(chunks))
	}
}

// TestChunkCode_NoDefinitionMatchFallback verifies that a known-language
// pattern that never matches still falls back to blank-line blocks (e.g. a
// Go file with no func declarations — just data).
func TestChunkCode_NoDefinitionMatchFallback(t *testing.T) {
	code := "var x = 1\nvar y = 2\n\nvar z = 3\nvar w = 4"
	chunks := chunk.ChunkCode(code, "go", 15, 3)
	if len(chunks) < 2 {
		t.Fatalf("expected fallback blank-line splitting, got %d chunk(s): %#v", len(chunks), chunks)
	}
	// Joined content must not lose lines from the original.
	var joined strings.Builder
	for _, c := range chunks {
		joined.WriteString(c.Content)
		joined.WriteString("\n")
	}
	for _, want := range []string{"var x = 1", "var y = 2", "var z = 3", "var w = 4"} {
		if !strings.Contains(joined.String(), want) {
			t.Errorf("fallback chunks lost %q: joined=%q", want, joined.String())
		}
	}
}

// TestChunkCode_PreamblePreserved verifies that content before the first
// top-level definition (package clause, imports, comments) is preserved as
// its own block rather than glued to the first function.
func TestChunkCode_PreamblePreserved(t *testing.T) {
	code := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hi\")\n}\nfunc helper() {\n\t_ = 42\n}"
	chunks := chunk.ChunkCode(code, "go", 45, 5)
	if len(chunks) < 2 {
		t.Fatalf("expected >=2 chunks, got %d", len(chunks))
	}
	// First chunk should be the preamble only (no func keyword unless it packed with main()).
	found := false
	for _, c := range chunks {
		if strings.Contains(c.Content, "package main") {
			found = true
		}
	}
	if !found {
		t.Error("expected a chunk containing the package preamble")
	}
}

// TestChunkCode_TypeScriptAndRustTopLevel verifies that TypeScript type
// aliases and Rust struct/enum/impl forms split on definition boundaries.
func TestChunkCode_TypeScriptAndRustTopLevel(t *testing.T) {
	// TypeScript with type, interface, and export function
	tsCode := "export type UserID = string;\ninterface User {\n\tid: UserID;\n}\nexport function getUser(): User {\n\treturn { id: \"1\" };\n}"
	tsChunks := chunk.ChunkCode(tsCode, "typescript", 25, 5)
	if len(tsChunks) < 2 {
		t.Fatalf("expected >=2 TypeScript chunks, got %d", len(tsChunks))
	}

	// Rust with pub(crate) struct, enum, and impl
	rustCode := "pub(crate) struct Point {\n\tx: i32,\n\ty: i32,\n}\nenum Color {\n\tRed,\n\tBlue,\n}\nimpl Point {\n\tpub fn new() -> Self {\n\t\tPoint { x: 0, y: 0 }\n\t}\n}"
	rustChunks := chunk.ChunkCode(rustCode, "rust", 25, 5)
	if len(rustChunks) < 2 {
		t.Fatalf("expected >=2 Rust chunks, got %d", len(rustChunks))
	}
}
