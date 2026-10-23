package search

import (
	"testing"
)

func TestChunkTypeConstants(t *testing.T) {
	if ChunkTypeText != 0 {
		t.Errorf("ChunkTypeText = %d, want 0", ChunkTypeText)
	}
	if ChunkTypeImage != 1 {
		t.Errorf("ChunkTypeImage = %d, want 1", ChunkTypeImage)
	}
}

func TestResultInstantiation(t *testing.T) {
	res := Result{
		DocumentID: 100,
		ChunkID:    200,
		Seq:        1,
		Title:      "Test Title",
		Path:       "/path/to/file.txt",
		Collection: "default",
		Content:    "Sample content",
		Score:      0.95,
		ChunkType:  ChunkTypeText,
		ImagePath:  "",
		StartLine:  1,
		EndLine:    10,
	}

	if res.DocumentID != 100 || res.ChunkID != 200 || res.Title != "Test Title" {
		t.Errorf("Result struct fields not matching expected values: %+v", res)
	}
	if res.ChunkType != ChunkTypeText {
		t.Errorf("Result ChunkType = %v, want ChunkTypeText", res.ChunkType)
	}
}
