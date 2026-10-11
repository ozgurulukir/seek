package indexer

import (
	"context"
	"errors"
	"testing"

	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/store"
)

type mockIndexWriter struct {
	upsertAndReplaceFn func(context.Context, store.DocumentIndex) (int64, error)
	upsertAndAppendFn  func(context.Context, store.DocumentIndex) (int64, error)
}

func (m *mockIndexWriter) UpsertAndReplaceIndex(ctx context.Context, doc store.DocumentIndex) (int64, error) {
	if m.upsertAndReplaceFn != nil {
		return m.upsertAndReplaceFn(ctx, doc)
	}
	return 0, nil
}

func (m *mockIndexWriter) UpsertAndAppendIndex(ctx context.Context, doc store.DocumentIndex) (int64, error) {
	if m.upsertAndAppendFn != nil {
		return m.upsertAndAppendFn(ctx, doc)
	}
	return 0, nil
}

func TestSourceHandlerFunc_Sync(t *testing.T) {
	called := false
	var fn sourceHandlerFunc = func(ctx context.Context, deps *HandlerDeps, col *store.Collection) error {
		called = true
		return errors.New("custom error")
	}

	err := fn.Sync(context.Background(), &HandlerDeps{}, &store.Collection{Name: "test"})
	if !called {
		t.Errorf("expected sourceHandlerFunc to be executed")
	}
	if err == nil || err.Error() != "custom error" {
		t.Errorf("expected 'custom error', got %v", err)
	}
}

func TestBindSourceHandler_ValidationErrors(t *testing.T) {
	dummyFn := func(idx *Indexer, col *store.Collection) error {
		return nil
	}
	handler := bindSourceHandler(dummyFn)
	ctx := context.Background()
	col := &store.Collection{Name: "test"}
	writer := &mockIndexWriter{}
	indexer := New(&config.AppConfig{}, nil)

	tests := []struct {
		name    string
		deps    *HandlerDeps
		wantErr string
	}{
		{
			name:    "nil deps",
			deps:    nil,
			wantErr: "source handler: missing indexer dependency",
		},
		{
			name:    "nil Indexer",
			deps:    &HandlerDeps{Writer: writer},
			wantErr: "source handler: missing indexer dependency",
		},
		{
			name:    "nil Writer",
			deps:    &HandlerDeps{Indexer: indexer},
			wantErr: "source handler: missing writer dependency",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := handler.Sync(ctx, tt.deps, col)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if err.Error() != tt.wantErr {
				t.Errorf("expected error %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}

func TestBindSourceHandler_Success(t *testing.T) {
	called := false
	expectedErr := errors.New("handler execution error")
	dummyFn := func(idx *Indexer, col *store.Collection) error {
		called = true
		if idx.writer == nil {
			t.Errorf("expected writer to be set on indexer")
		}
		if idx.ctx() == nil {
			t.Errorf("expected context to be set on indexer")
		}
		if col.Name != "test-col" {
			t.Errorf("expected collection name 'test-col', got %q", col.Name)
		}
		return expectedErr
	}

	handler := bindSourceHandler(dummyFn)
	ctx := context.WithValue(context.Background(), "key", "value")
	writer := &mockIndexWriter{}
	idx := New(&config.AppConfig{}, nil)
	deps := &HandlerDeps{
		Indexer: idx,
		Writer:  writer,
	}
	col := &store.Collection{Name: "test-col"}

	err := handler.Sync(ctx, deps, col)
	if !called {
		t.Errorf("expected handler function to be called")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
}

func TestSyncHandlers_Registration(t *testing.T) {
	expectedTypes := []store.CollectionType{
		store.CollectionTypeMarkdown,
		store.CollectionTypeClaude,
		store.CollectionTypeCodex,
		store.CollectionTypeImages,
		store.CollectionTypePDF,
		store.CollectionTypeDocuments,
		store.CollectionTypeCode,
		store.CollectionTypeParser,
	}

	for _, colType := range expectedTypes {
		handler, exists := syncHandlers[colType]
		if !exists {
			t.Errorf("expected syncHandlers to have registered handler for collection type %q", colType)
		}
		if handler == nil {
			t.Errorf("handler for collection type %q is nil", colType)
		}
	}

	if len(syncHandlers) != len(expectedTypes) {
		t.Errorf("expected %d handlers registered, got %d", len(expectedTypes), len(syncHandlers))
	}
}
