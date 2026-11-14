package embed

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkEmbedImagesInBatches(b *testing.B) {
	tmpDir := b.TempDir()
	// Create 100 dummy image files (~10KB each)
	numFiles := 100
	paths := make([]string, numFiles)
	data := make([]byte, 10240)
	for i := 0; i < numFiles; i++ {
		imgPath := filepath.Join(tmpDir, fmt.Sprintf("test_%d.png", i))
		if err := os.WriteFile(imgPath, data, 0644); err != nil {
			b.Fatalf("failed to write test image: %v", err)
		}
		paths[i] = imgPath
	}

	items := make([]ImageBatchItem, numFiles)
	for i := range items {
		items[i] = ImageBatchItem{
			ImagePath: paths[i],
			Text:      "test caption",
		}
	}

	c := NewVLClient("k", "model", 8, "https://provider.example/v1/embeddings", TaskPrefix{})

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Mock onBatch to avoid real network call
		_, _ = c.EmbedImagesInBatches(items, 100, 0, func(batchStart int, embeddings [][]float32, validIndices []int) error {
			return nil
		})
	}
}
