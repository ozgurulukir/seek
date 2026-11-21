package app

import (
	"context"
	"strings"
	"testing"

	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/indexer"
	"github.com/ozgurulukir/seek/internal/pipeline"
	"github.com/ozgurulukir/seek/internal/store"
)

// TestCollectionServiceNilDependencyGuards pins the canonical guard behavior:
// every exported method checks exactly the dependencies it uses and reports
// one uniform "not configured" error naming what is missing (#191).
func TestCollectionServiceNilDependencyGuards(t *testing.T) {
	var nilSvc *CollectionService
	if _, err := nilSvc.List(context.Background()); err == nil {
		t.Error("nil receiver List: expected error")
	}
	if err := nilSvc.Rename(context.Background(), "a", "b"); err == nil {
		t.Error("nil receiver Rename: expected error")
	}

	// A service with only a store must refuse Reindex (pipeline missing),
	// SyncPath (pipeline missing), and Backfill (indexer missing) with the
	// uniform message naming the missing dependency.
	svc := &CollectionService{}
	for name, run := range map[string]func() error{
		"Reindex": func() error {
			_, err := svc.Reindex(context.Background(), "x", ReindexOptions{}, nil)
			return err
		},
		"SyncPath": func() error {
			_, err := svc.SyncPath(context.Background(), "x", "p", pipeline.Options{}, nil)
			return err
		},
		"Backfill": func() error {
			_, err := svc.Backfill(context.Background(), "x", nil)
			return err
		},
	} {
		err := run()
		if err == nil {
			t.Errorf("%s: expected not-configured error", name)
			continue
		}
		if !strings.Contains(err.Error(), "collection service: not configured") {
			t.Errorf("%s: unexpected message: %v", name, err)
		}
	}

	if _, err := svc.Reindex(context.Background(), "x", ReindexOptions{}, nil); !strings.Contains(err.Error(), "missing store, pipeline") {
		t.Errorf("Reindex should name the missing dependencies: %v", err)
	}
	if _, err := svc.Backfill(context.Background(), "x", nil); !strings.Contains(err.Error(), "missing store, indexer") {
		t.Errorf("Backfill should name the missing dependencies: %v", err)
	}

	// ReindexAll previously had no nil-dependency guard at all.
	var nilAll *CollectionService
	if _, err := nilAll.ReindexAll(context.Background(), ReindexOptions{AllowVectorSpaceChange: true}, nil); err == nil {
		t.Error("nil receiver ReindexAll: expected error")
	}
	if _, err := (&CollectionService{}).ReindexAll(context.Background(), ReindexOptions{AllowVectorSpaceChange: true}, nil); err == nil {
		t.Error("store-less ReindexAll: expected error")
	}
}

// TestAddCollectionServiceNilDependencyGuards pins the add-service guard.
func TestAddCollectionServiceNilDependencyGuards(t *testing.T) {
	var nilSvc *AddCollectionService
	if err := nilSvc.Add(AddRequest{}); err == nil {
		t.Error("nil receiver Add: expected error")
	}
	if err := (&AddCollectionService{}).Add(AddRequest{}); err == nil {
		t.Error("empty Add service: expected error")
	} else if !strings.Contains(err.Error(), "missing store") {
		t.Errorf("Add should name the missing dependency: %v", err)
	}
}

// TestRequireDepsSatisfiedReturnsNil pins the satisfied path of the shared
// guard: a fully-populated service passes requireDeps with no error. This
// path had no test and a first draft of the shared guard broke it (the
// error was returned unconditionally) — CI caught what the earlier gate
// run missed.
func TestRequireDepsSatisfiedReturnsNil(t *testing.T) {
	svc := &CollectionService{
		store:    &store.Store{},
		cfg:      &config.AppConfig{},
		pipeline: &pipeline.Pipeline{},
		indexer:  &indexer.Indexer{},
	}
	for _, names := range [][]string{
		{"store"},
		{"store", "pipeline"},
		{"store", "pipeline", "indexer"},
	} {
		if err := svc.requireDeps(names...); err != nil {
			t.Errorf("requireDeps(%v) with all deps set: unexpected error %v", names, err)
		}
	}
	add := &AddCollectionService{store: &store.Store{}, cfg: &config.AppConfig{}}
	if err := add.requireDeps("store", "cfg"); err != nil {
		t.Errorf("AddCollectionService.requireDeps with deps set: unexpected error %v", err)
	}
}
