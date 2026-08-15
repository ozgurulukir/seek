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
