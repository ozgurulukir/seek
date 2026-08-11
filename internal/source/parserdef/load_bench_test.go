package parserdef

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkList(b *testing.B) {
	tempDir := b.TempDir()

	for i := 0; i < 50; i++ {
		filename := filepath.Join(tempDir, fmt.Sprintf("schema%d.yaml", i))
		data := []byte(`
type: jsonl
sql_table: test
fields:
  - name: id
    type: integer
    source: .id
`)
		os.WriteFile(filename, data, 0644)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = listFrom(tempDir)
	}
}
