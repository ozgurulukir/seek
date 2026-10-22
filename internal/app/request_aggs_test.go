package app

import (
	"context"
	"fmt"
	"testing"

	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/search"
	"github.com/ozgurulukir/seek/internal/store"
)

func TestRunAggs_TermsBounds(t *testing.T) {
	ctx := context.Background()
	runtime := newRequestTestRuntimeWithConfig(t, config.Config{
		Aggregations: config.AggregationConfig{Enabled: true},
	})
	col, err := runtime.Store.CreateCollection("docs", store.CollectionTypeMarkdown, "/tmp", "**/*.md")
	if err != nil {
		t.Fatal(err)
	}

	for i := 1; i <= 15; i++ {
		name := fmt.Sprintf("doc%02d.md", i)
		path := fmt.Sprintf("/tmp/%s", name)
		docID, err := runtime.Store.UpsertDocument(col.ID, path, name, fmt.Sprintf("h%d", i), 1, 10)
		if err != nil {
			t.Fatal(err)
		}
		if err := runtime.Store.UpsertFTS(docID, name, "content"); err != nil {
			t.Fatal(err)
		}
	}

	// 1. Default terms limit (10)
	aggs, err := runtime.RunAggs(ctx, SearchRequest{Aggs: []string{"path:terms"}})
	if err != nil {
		t.Fatalf("RunAggs: %v", err)
	}
	if got := len(aggs["path:terms"]); got != search.DefaultAggregationBucketLimit {
		t.Fatalf("default path:terms buckets = %d, want %d", got, search.DefaultAggregationBucketLimit)
	}

	// 2. Explicit override smaller (3)
	aggs, err = runtime.RunAggs(ctx, SearchRequest{Aggs: []string{"path:terms:3"}})
	if err != nil {
		t.Fatalf("RunAggs: %v", err)
	}
	if got := len(aggs["path:terms:3"]); got != 3 {
		t.Fatalf("explicit path:terms:3 buckets = %d, want 3", got)
	}

	// 3. Explicit override larger (20) -> all 15
	aggs, err = runtime.RunAggs(ctx, SearchRequest{Aggs: []string{"path:terms:20"}})
	if err != nil {
		t.Fatalf("RunAggs: %v", err)
	}
	if got := len(aggs["path:terms:20"]); got != 15 {
		t.Fatalf("explicit path:terms:20 buckets = %d, want 15", got)
	}

	// 4. Invalid spec (size <= 0) returns error
	if _, err := runtime.RunAggs(ctx, SearchRequest{Aggs: []string{"path:terms:0"}}); err == nil {
		t.Fatalf("expected error for path:terms:0, got nil")
	}
	if _, err := runtime.RunAggs(ctx, SearchRequest{Aggs: []string{"path:terms:-5"}}); err == nil {
		t.Fatalf("expected error for path:terms:-5, got nil")
	}
}

func TestRunAggs_ConfiguredDefaultBucketLimit(t *testing.T) {
	ctx := context.Background()
	runtime := newRequestTestRuntimeWithConfig(t, config.Config{
		Aggregations: config.AggregationConfig{
			Enabled:            true,
			DefaultBucketLimit: 5,
		},
	})
	col, err := runtime.Store.CreateCollection("docs", store.CollectionTypeMarkdown, "/tmp", "**/*.md")
	if err != nil {
		t.Fatal(err)
	}

	for i := 1; i <= 15; i++ {
		name := fmt.Sprintf("doc%02d.md", i)
		path := fmt.Sprintf("/tmp/%s", name)
		docID, err := runtime.Store.UpsertDocument(col.ID, path, name, fmt.Sprintf("h%d", i), 1, 10)
		if err != nil {
			t.Fatal(err)
		}
		if err := runtime.Store.UpsertFTS(docID, name, "content"); err != nil {
			t.Fatal(err)
		}
	}

	// Unspecified terms aggregation should use configured default bucket limit (5)
	aggs, err := runtime.RunAggs(ctx, SearchRequest{Aggs: []string{"path:terms"}})
	if err != nil {
		t.Fatalf("RunAggs: %v", err)
	}
	if got := len(aggs["path:terms"]); got != 5 {
		t.Fatalf("configured default path:terms buckets = %d, want 5", got)
	}

	// Explicit override (8) should take precedence over configured default (5)
	aggs, err = runtime.RunAggs(ctx, SearchRequest{Aggs: []string{"path:terms:8"}})
	if err != nil {
		t.Fatalf("RunAggs: %v", err)
	}
	if got := len(aggs["path:terms:8"]); got != 8 {
		t.Fatalf("explicit path:terms:8 buckets = %d, want 8", got)
	}
}
