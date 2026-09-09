package indexer

import (
	"context"
	"testing"

	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/store"
)

func TestSyncCollectionWithReport_ClassifiesUnsupportedCollection(t *testing.T) {
	idx := New(&config.AppConfig{}, nil)
	report, err := idx.SyncCollectionWithReport(context.Background(), &store.Collection{
		Name: "future",
		Type: store.CollectionType("new-format"),
	})
	if err == nil {
		t.Fatal("expected unknown collection error")
	}
	if report.Unsupported != 1 || report.Failed != 0 {
		t.Fatalf("unexpected report: %+v", report)
	}
	if len(report.Errors) != 1 || report.Errors[0].Kind != "unsupported_collection" {
		t.Fatalf("missing unsupported error: %+v", report.Errors)
	}
}

func TestSyncCollectionWithReport_ClassifiesInvalidCollection(t *testing.T) {
	idx := New(&config.AppConfig{}, nil)
	report, err := idx.SyncCollectionWithReport(context.Background(), nil)
	if err == nil {
		t.Fatal("expected nil collection error")
	}
	if report.Failed != 1 || report.Unsupported != 0 {
		t.Fatalf("unexpected report: %+v", report)
	}
}
