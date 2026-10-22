package store

import (
	"context"
	"fmt"
	"testing"
)

func TestTermsAggregationBounds(t *testing.T) {
	s := newTestStore(t)
	col, err := s.CreateCollection("docs", CollectionTypeMarkdown, "/docs", "**/*.md")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}

	// Create 15 documents with distinct paths, authors (exact fast field),
	// and tags (membership fast field).
	const totalDocs = 15
	for i := 1; i <= totalDocs; i++ {
		path := fmt.Sprintf("/docs/doc%02d.md", i)
		docID, err := s.UpsertDocument(col.ID, path, path, fmt.Sprintf("hash%d", i), 1, 10)
		if err != nil {
			t.Fatalf("UpsertDocument %d: %v", i, err)
		}
		// Exact fast field: author01 .. author15
		if err := s.FastFields().Set(docID, "author", fmt.Sprintf("author%02d", i)); err != nil {
			t.Fatalf("set author %d: %v", i, err)
		}
		// Membership fast field: tag01 .. tag15
		if err := s.FastFields().Set(docID, "tags", fmt.Sprintf("tag%02d", i)); err != nil {
			t.Fatalf("set tags %d: %v", i, err)
		}
	}

	ctx := context.Background()

	cases := []struct {
		name      string
		spec      AggregationSpec
		wantCount int
	}{
		// 1. Documents column path ("path")
		{name: "column default limit", spec: AggregationSpec{Type: "terms", Field: "path"}, wantCount: DefaultAggregationBucketLimit},
		{name: "column explicit override smaller", spec: AggregationSpec{Type: "terms", Field: "path", Size: 3}, wantCount: 3},
		{name: "column explicit override larger", spec: AggregationSpec{Type: "terms", Field: "path", Size: 20}, wantCount: 15},
		{name: "column zero size falls back to default", spec: AggregationSpec{Type: "terms", Field: "path", Size: 0}, wantCount: DefaultAggregationBucketLimit},
		{name: "column negative size falls back to default", spec: AggregationSpec{Type: "terms", Field: "path", Size: -5}, wantCount: DefaultAggregationBucketLimit},

		// 2. Exact fast field path ("author")
		{name: "exact fast field default limit", spec: AggregationSpec{Type: "terms", Field: "author"}, wantCount: DefaultAggregationBucketLimit},
		{name: "exact fast field explicit override smaller", spec: AggregationSpec{Type: "terms", Field: "author", Size: 4}, wantCount: 4},
		{name: "exact fast field explicit override larger", spec: AggregationSpec{Type: "terms", Field: "author", Size: 25}, wantCount: 15},
		{name: "exact fast field zero size falls back to default", spec: AggregationSpec{Type: "terms", Field: "author", Size: 0}, wantCount: DefaultAggregationBucketLimit},
		{name: "exact fast field negative size falls back to default", spec: AggregationSpec{Type: "terms", Field: "author", Size: -10}, wantCount: DefaultAggregationBucketLimit},

		// 3. Membership fast field path ("tags")
		{name: "membership fast field default limit", spec: AggregationSpec{Type: "terms", Field: "tags"}, wantCount: DefaultAggregationBucketLimit},
		{name: "membership fast field explicit override smaller", spec: AggregationSpec{Type: "terms", Field: "tags", Size: 5}, wantCount: 5},
		{name: "membership fast field explicit override larger", spec: AggregationSpec{Type: "terms", Field: "tags", Size: 30}, wantCount: 15},
		{name: "membership fast field zero size falls back to default", spec: AggregationSpec{Type: "terms", Field: "tags", Size: 0}, wantCount: DefaultAggregationBucketLimit},
		{name: "membership fast field negative size falls back to default", spec: AggregationSpec{Type: "terms", Field: "tags", Size: -2}, wantCount: DefaultAggregationBucketLimit},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			buckets, err := s.ExecuteAggregationContext(ctx, tc.spec, nil)
			if err != nil {
				t.Fatalf("ExecuteAggregationContext: %v", err)
			}
			if len(buckets) != tc.wantCount {
				t.Fatalf("got %d buckets, want %d", len(buckets), tc.wantCount)
			}
		})
	}
}
