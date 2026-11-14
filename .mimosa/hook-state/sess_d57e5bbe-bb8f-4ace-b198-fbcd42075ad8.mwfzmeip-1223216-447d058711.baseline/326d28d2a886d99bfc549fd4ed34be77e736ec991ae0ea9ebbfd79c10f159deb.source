package chunk

import (
	"fmt"
	"strings"
	"testing"
)

func BenchmarkChunkCode(b *testing.B) {
	// Generate a 2000-line Go source file
	var sb strings.Builder
	sb.WriteString("package main\n\nimport \"fmt\"\n\n")
	for i := 1; i <= 200; i++ {
		sb.WriteString(fmt.Sprintf("// Function %d does something useful\nfunc Function%d(a int, b string) (int, error) {\n\t// line 1\n\t// line 2\n\t// line 3\n\t// line 4\n\treturn a + %d, nil\n}\n\n", i, i, i))
	}
	content := sb.String()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = ChunkCode(content, "go", 1000, 100)
	}
}
