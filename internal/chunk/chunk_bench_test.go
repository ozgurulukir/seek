package chunk

import (
	"fmt"
	"strings"
	"testing"
)

func BenchmarkAssignLineNumbers(b *testing.B) {
	// Create a 1000-line markdown document with multiple sections and paragraphs
	var sb strings.Builder
	for i := 1; i <= 200; i++ {
		sb.WriteString(fmt.Sprintf("# Section %d\n\nThis is paragraph 1 of section %d with some text.\nThis is line 2 of paragraph 1.\n\nThis is paragraph 2 of section %d.\nLine 2 of para 2.\nLine 3 of para 2.\n\n", i, i, i))
	}
	content := sb.String()

	chunks := ChunkMarkdown(content, 500, 50)
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Pass a copy of chunks so seq/lines are recalculated cleanly
		chunksCopy := make([]Chunk, len(chunks))
		copy(chunksCopy, chunks)
		AssignLineNumbers(content, chunksCopy)
	}
}
