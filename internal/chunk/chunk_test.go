package chunk

import (
	"strings"
	"testing"
)

func TestChunkMarkdownSmallContent(t *testing.T) {
	content := "# Header\n\nSome short text."
	chunks := ChunkMarkdown(content, 1000, 100)

	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	c := chunks[0]
	if c.Seq != 0 {
		t.Errorf("expected seq=0, got %d", c.Seq)
	}
	if c.Type != ChunkText {
		t.Errorf("expected ChunkText, got %v", c.Type)
	}
	if !strings.Contains(c.Content, "Some short text.") {
		t.Errorf("chunk content missing text: %q", c.Content)
	}
}

func TestChunkMarkdownSplitsByHeaders(t *testing.T) {
	content := "# First Section\n\nBody one.\n\n# Second Section\n\nBody two.\n\n# Third Section\n\nBody three."
	chunks := ChunkMarkdown(content, 1000, 100)

	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks (one per section), got %d", len(chunks))
	}

	for i, c := range chunks {
		if c.Seq != i {
			t.Errorf("chunk %d: expected seq=%d, got %d", i, i, c.Seq)
		}
		if !strings.Contains(c.Content, "# ") {
			t.Errorf("chunk %d missing header: %q", i, c.Content)
		}
	}
}

func TestChunkMarkdownEmpty(t *testing.T) {
	chunks := ChunkMarkdown("", 1000, 100)
	if len(chunks) != 0 {
		t.Errorf("expected 0 chunks for empty input, got %d", len(chunks))
	}
}

func TestChunkMarkdownWhitespaceOnly(t *testing.T) {
	chunks := ChunkMarkdown("   \n\n  \n", 1000, 100)
	if len(chunks) != 0 {
		t.Errorf("expected 0 chunks for whitespace-only input, got %d", len(chunks))
	}
}

func TestChunkMarkdownDefaults(t *testing.T) {
	// Zero/negative maxSize and overlap should fall back to defaults and not panic.
	// Use multiple paragraphs so splitBySize splits them.
	var sb strings.Builder
	for i := 0; i < 20; i++ {
		sb.WriteString(strings.Repeat("word ", 100)) // 400 chars per paragraph
		sb.WriteString("\n\n")
	}
	content := "# H\n\n" + sb.String()
	chunks := ChunkMarkdown(content, 0, 0)
	if len(chunks) == 0 {
		t.Fatal("expected at least 1 chunk with default parameters")
	}
	for _, c := range chunks {
		if c.Content == "" {
			t.Errorf("chunk %d has empty content", c.Seq)
		}
	}
	// No individual paragraph is >1000, but combined they exceed it,
	// so we expect multiple chunks from a single section.
	if len(chunks) < 2 {
		t.Errorf("expected multiple chunks from a large multi-paragraph section, got %d", len(chunks))
	}
}

func TestChunkMarkdownLargeSectionSplitBySize(t *testing.T) {
	// A section with many paragraphs that together exceed maxSize must be split.
	var sb strings.Builder
	for i := 0; i < 10; i++ {
		sb.WriteString(strings.Repeat("alpha ", 50)) // 250 chars per paragraph
		sb.WriteString("\n\n")
	}
	content := sb.String() // ~2500 chars total, no headers
	maxSize := 500
	overlap := 50
	chunks := ChunkMarkdown(content, maxSize, overlap)

	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks for large multi-paragraph content, got %d", len(chunks))
	}

	// Each chunk must be within maxSize (paragraph split keeps each chunk under size).
	for _, c := range chunks {
		if len(c.Content) > maxSize {
			t.Errorf("chunk %d exceeds maxSize=%d: %d chars", c.Seq, maxSize, len(c.Content))
		}
		if c.Content == "" {
			t.Errorf("chunk %d has empty content", c.Seq)
		}
	}
}

func TestChunkMarkdownSingleLargeParagraph(t *testing.T) {
	// A single paragraph larger than maxSize is returned whole (splitBySize
	// only splits at paragraph boundaries) — verify no panic and one chunk.
	content := strings.Repeat("x", 5000) // 5000-char single paragraph
	chunks := ChunkMarkdown(content, 500, 50)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk for single paragraph, got %d", len(chunks))
	}
	if chunks[0].Content != content {
		t.Errorf("expected content preserved, got %d chars", len(chunks[0].Content))
	}
}

func TestChunkMarkdownSeqIsContinuous(t *testing.T) {
	// Two sections: first has many paragraphs (splits into multiple chunks),
	// second is short. Verify seq values are continuous.
	var sb strings.Builder
	for i := 0; i < 8; i++ {
		sb.WriteString(strings.Repeat("para", 80)) // ~320 chars
		sb.WriteString("\n\n")
	}
	content := "# A\n\n" + sb.String() + "# B\n\nshort b"
	chunks := ChunkMarkdown(content, 500, 50)

	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}
	for i, c := range chunks {
		if c.Seq != i {
			t.Errorf("expected continuous seq, chunk %d has seq=%d", i, c.Seq)
		}
	}
}

func TestChunkConversation(t *testing.T) {
	content := "line one\nline two\nline three\nline four\nline five"
	chunks := ChunkConversation(content, 10) // small maxSize to force multiple chunks

	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	for i, c := range chunks {
		if c.Seq != i {
			t.Errorf("expected seq=%d, got %d", i, c.Seq)
		}
		if c.Type != ChunkText {
			t.Errorf("expected ChunkText, got %v", c.Type)
		}
		if strings.TrimSpace(c.Content) == "" {
			t.Errorf("chunk %d has empty content", i)
		}
	}
}

func TestChunkConversationDefaults(t *testing.T) {
	content := "short content"
	chunks := ChunkConversation(content, 0)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Content != content {
		t.Errorf("expected content %q, got %q", content, chunks[0].Content)
	}
}

func TestChunkConversationEmpty(t *testing.T) {
	chunks := ChunkConversation("", 100)
	if len(chunks) != 0 {
		t.Errorf("expected 0 chunks, got %d", len(chunks))
	}
}

func TestSplitByHeaders(t *testing.T) {
	content := "intro line\n# Header One\nbody one\n# Header Two\nbody two"
	sections := splitByHeaders(content)

	// splitByHeaders splits before each header when there's accumulated content,
	// so "intro line" becomes its own section, then each header section follows.
	if len(sections) != 3 {
		t.Fatalf("expected 3 sections, got %d", len(sections))
	}
	if !strings.Contains(sections[0], "intro line") {
		t.Errorf("first section missing intro: %q", sections[0])
	}
	if !strings.Contains(sections[1], "# Header One") {
		t.Errorf("second section missing header one: %q", sections[1])
	}
	if !strings.Contains(sections[2], "# Header Two") {
		t.Errorf("third section missing header two: %q", sections[2])
	}
}

func TestSplitBySizeOverlap(t *testing.T) {
	// Multiple paragraphs that together exceed maxSize, with overlap.
	paragraphs := make([]string, 5)
	for i := range paragraphs {
		paragraphs[i] = strings.Repeat("para"+string(rune('a'+i)), 50)
	}
	content := strings.Join(paragraphs, "\n\n")

	maxSize := 300
	overlap := 30
	parts := splitBySize(content, maxSize, overlap)

	if len(parts) < 2 {
		t.Fatalf("expected 2+ parts, got %d", len(parts))
	}
	for _, p := range parts {
		if len(p) > maxSize {
			t.Errorf("part exceeds maxSize=%d: %d chars", maxSize, len(p))
		}
	}
}

func TestSplitBySizeNoOverlap(t *testing.T) {
	paragraphs := make([]string, 4)
	for i := range paragraphs {
		paragraphs[i] = strings.Repeat("word", 60) // 240 chars
	}
	content := strings.Join(paragraphs, "\n\n")

	parts := splitBySize(content, 400, 0) // overlap=0

	if len(parts) < 2 {
		t.Fatalf("expected 2+ parts, got %d", len(parts))
	}
	// With overlap=0, no tail should be carried into the next part.
	for _, p := range parts {
		if len(p) > 400 {
			t.Errorf("part exceeds maxSize=400: %d chars", len(p))
		}
	}
}

func TestChunkMarkdown_ExcessiveOverlap(t *testing.T) {
	// Overlap >= maxSize should be gracefully capped without infinite loop or failure
	content := strings.Repeat("para one content line\n\n", 10)
	chunks := ChunkMarkdown(content, 100, 200)
	if len(chunks) == 0 {
		t.Fatal("expected chunks, got 0")
	}
}
