package parserdef

import (
	"fmt"
	"strings"
	"testing"
)

func BenchmarkJoinStrings(b *testing.B) {
	sizes := []int{5, 50, 500}
	for _, size := range sizes {
		parts := make([]string, size)
		for i := 0; i < size; i++ {
			parts[i] = fmt.Sprintf("line %d: some message text content here", i)
		}
		b.Run(fmt.Sprintf("size_%d", size), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = strings.Join(parts, "\n")
			}
		})
	}
}
