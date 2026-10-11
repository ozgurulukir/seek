package indexer

import (
	"context"
	"errors"
	"testing"

	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/store"
)

type mockIndexWriter struct {
	upsertAndReplaceFunc func(ctx context.Context, doc store.DocumentIndex) (int64, error)
	upsertAndAppendFunc  func(ctx context.Context, doc store.DocumentIndex) (int64, error)

	replaceCalled bool
	appendCalled  bool
	lastDoc       store.DocumentIndex
	lastCtx       context.Context
}

func (m *mockIndexWriter) UpsertAndReplaceIndex(ctx context.Context, doc store.DocumentIndex) (int64, error) {
	m.replaceCalled = true
	m.lastCtx = ctx
	m.lastDoc = doc
	if m.upsertAndReplaceFunc != nil {
		return m.upsertAndReplaceFunc(ctx, doc)
	}
	return 42, nil
}

func (m *mockIndexWriter) UpsertAndAppendIndex(ctx context.Context, doc store.DocumentIndex) (int64, error) {
	m.appendCalled = true
	m.lastCtx = ctx
	m.lastDoc = doc
	if m.upsertAndAppendFunc != nil {
		return m.upsertAndAppendFunc(ctx, doc)
	}
	return 84, nil
}

func TestWithIndexWriter(t *testing.T) {
	cfg := &config.AppConfig{}
	writer1 := &mockIndexWriter{}
	writer2 := &mockIndexWriter{}

	idx := NewWithDependencies(cfg, nil, nil, writer1)
	if idx.writer != writer1 {
		t.Errorf("NewWithDependencies did not set initial writer correctly")
	}

	returnedIdx := idx.WithIndexWriter(writer2)
	if returnedIdx != idx {
		t.Errorf("WithIndexWriter did not return fluent indexer instance")
	}
	if idx.writer != writer2 {
		t.Errorf("WithIndexWriter did not update writer")
	}
}

func TestWriteConversationDocument_ReplaceAndAppend(t *testing.T) {
	cfg := &config.AppConfig{}
	mock := &mockIndexWriter{}
	idx := NewWithDependencies(cfg, nil, nil, mock)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	idx.WithContext(ctx)

	doc := store.DocumentIndex{
		CollectionID: 10,
		Path:         "chat.json",
		Title:        "Session 1",
	}

	// fromLine == 0 -> replace mode
	docID, err := idx.writeConversationDocument(doc, 0)
	if err != nil {
		t.Fatalf("unexpected error on replace: %v", err)
	}
	if docID != 42 {
		t.Errorf("got docID %d, want 42", docID)
	}
	if !mock.replaceCalled {
		t.Errorf("expected UpsertAndReplaceIndex to be called")
	}
	if mock.lastDoc.Path != "chat.json" || mock.lastDoc.CollectionID != 10 {
		t.Errorf("document payload mismatch: %+v", mock.lastDoc)
	}
	if mock.lastCtx != ctx {
		t.Errorf("context was not passed to IndexWriter")
	}

	// fromLine > 0 -> append mode
	mock.replaceCalled = false
	docID, err = idx.writeConversationDocument(doc, 5)
	if err != nil {
		t.Fatalf("unexpected error on append: %v", err)
	}
	if docID != 84 {
		t.Errorf("got docID %d, want 84", docID)
	}
	if !mock.appendCalled {
		t.Errorf("expected UpsertAndAppendIndex to be called")
	}
}

func TestWriteConversationDocument_ErrorPropagation(t *testing.T) {
	cfg := &config.AppConfig{}
	wantErr := errors.New("database disk image is malformed")
	mock := &mockIndexWriter{
		upsertAndReplaceFunc: func(ctx context.Context, doc store.DocumentIndex) (int64, error) {
			return 0, wantErr
		},
		upsertAndAppendFunc: func(ctx context.Context, doc store.DocumentIndex) (int64, error) {
			return 0, wantErr
		},
	}
	idx := NewWithDependencies(cfg, nil, nil, mock)

	doc := store.DocumentIndex{Path: "test.json"}

	_, err := idx.writeConversationDocument(doc, 0)
	if !errors.Is(err, wantErr) {
		t.Errorf("replace error: got %v, want %v", err, wantErr)
	}

	_, err = idx.writeConversationDocument(doc, 10)
	if !errors.Is(err, wantErr) {
		t.Errorf("append error: got %v, want %v", err, wantErr)
	}
}
