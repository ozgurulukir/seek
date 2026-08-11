package indexer

import (
	"context"
	"testing"

	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/extractor"
	"github.com/ozgurulukir/seek/internal/store"
)

type mockExtractor struct{}

func (mockExtractor) Extract(ctx context.Context, path string) (extractor.Result, error) {
	return extractor.Result{}, nil
}

func (mockExtractor) Supports(path string) bool { return true }
func (mockExtractor) Name() string              { return "mock" }

func TestIndexer_WithExtractor(t *testing.T) {
	cfg := &config.AppConfig{}
	idx := New(cfg, nil)

	mockExt := &mockExtractor{}

	// Check fluent return
	returnedIdx := idx.WithExtractor(mockExt)
	if returnedIdx != idx {
		t.Errorf("WithExtractor did not return the indexer instance")
	}

	// Check internal state
	if idx.ext != mockExt {
		t.Errorf("WithExtractor did not set the extractor correctly")
	}

	// Check extractorFor resolution priority
	col := &store.Collection{Backend: "some-backend"}
	resolvedExt, err := idx.extractorFor(col)
	if err != nil {
		t.Fatalf("extractorFor returned unexpected error: %v", err)
	}

	if resolvedExt != mockExt {
		t.Errorf("extractorFor did not prioritize the overridden extractor, got: %v", resolvedExt)
	}

	// Revert the override
	idx.WithExtractor(nil)
	if idx.ext != nil {
		t.Errorf("WithExtractor did not revert the extractor to nil")
	}
}
