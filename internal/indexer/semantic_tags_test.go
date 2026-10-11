package indexer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ozgurulukir/seek/internal/semantic"
	"github.com/ozgurulukir/seek/internal/store"
)

// TestSemanticChunkCap: verifies that when more than 200 (semanticChunkCap) non-empty
// chunks are provided, only the first 200 chunks are sent to the semantic tag service.
func TestSemanticChunkCap(t *testing.T) {
	tmp := t.TempDir()
	receivedChunkCount := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.Write([]byte(`{"status":"ok","version":"0.2.0","models":{"lid":true,"ner":true,"keyphrase":true,"topic":true}}`))
		case "/tag":
			var req struct {
				Chunks []struct {
					ID   int    `json:"id"`
					Text string `json:"text"`
				} `json:"chunks"`
				MaxTags int `json:"max_tags"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			receivedChunkCount = len(req.Chunks)

			type result struct {
				ID   int      `json:"id"`
				Tags []string `json:"tags"`
			}
			out := struct {
				Results    []result `json:"results"`
				Errors     []any    `json:"errors"`
				CorpusLang string   `json:"corpus_lang"`
			}{
				Results:    []result{{ID: 0, Tags: []string{"test-tag"}}},
				Errors:     []any{},
				CorpusLang: "en",
			}
			w.Write(mustJSON(t, out))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	cfg := cfgFromTest(t, tmp, filepath.Join(tmp, "test.db"))
	cfg.Config.Semantic.Enabled = true
	cfg.Config.Semantic.BaseURL = srv.URL

	idx := New(cfg, nil)
	idx.WithLogger(nopLogger{})

	// Generate 250 non-empty chunks.
	var chunks []store.IndexChunk
	for i := 0; i < 250; i++ {
		chunks = append(chunks, store.IndexChunk{
			Seq:     i,
			Content: fmt.Sprintf("Chunk content %d", i),
		})
	}

	fields := idx.semanticFastFields(context.Background(), "large_doc.pdf", chunks)
	if fields == nil {
		t.Fatal("expected non-nil fields")
	}

	if receivedChunkCount != semanticChunkCap {
		t.Errorf("receivedChunkCount = %d, want %d (semanticChunkCap)", receivedChunkCount, semanticChunkCap)
	}
}

// TestSemanticTagFormattingDeduplicationAndSorting: verifies deduplication of tags,
// whitespace trimming and sorting of topics, and formatting/deduplication/sorting of entities.
func TestSemanticTagFormattingDeduplicationAndSorting(t *testing.T) {
	tmp := t.TempDir()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.Write([]byte(`{"status":"ok","version":"0.2.0","models":{"lid":true,"ner":true,"keyphrase":true,"topic":true}}`))
		case "/tag":
			out := map[string]any{
				"results": []map[string]any{
					{
						"id":   0,
						"tags": []string{"golang", "concurrency", "golang", "", "channels"},
						"topics": []map[string]any{
							{"label": "  go concurrency  ", "score": 0.9},
							{"label": "  ", "score": 0.8},
							{"label": "algorithms", "score": 0.7},
							{"label": "go concurrency", "score": 0.9}, // duplicate after trim
						},
						"entities": []map[string]any{
							{"text": "Go", "type": "LOC"},
							{"text": "OpenAI", "type": "ORG"},
							{"text": "Go", "type": "LOC"}, // duplicate pair
						},
					},
				},
				"errors":      []any{},
				"corpus_lang": "en",
			}
			w.Write(mustJSON(t, out))
		}
	}))
	defer srv.Close()

	cfg := cfgFromTest(t, tmp, filepath.Join(tmp, "test.db"))
	cfg.Config.Semantic.Enabled = true
	cfg.Config.Semantic.BaseURL = srv.URL

	idx := New(cfg, nil)
	idx.WithLogger(nopLogger{})

	fields := idx.semanticFastFields(context.Background(), "doc.md", textChunks("sample text"))
	if fields == nil {
		t.Fatal("expected non-nil fields")
	}

	// Tags: empty strings skipped, duplicates removed, insertion order preserved.
	if got, want := fields["tags"], "golang,concurrency,channels"; got != want {
		t.Errorf("tags = %q, want %q", got, want)
	}

	// Topics: whitespace trimmed, empty skipped, duplicates removed, sorted alphabetically.
	if got, want := fields["topics"], "algorithms,go concurrency"; got != want {
		t.Errorf("topics = %q, want %q", got, want)
	}

	// Entities: formatted as "TYPE:Text", duplicates removed, sorted alphabetically.
	if got, want := fields["entities"], "LOC:Go,ORG:OpenAI"; got != want {
		t.Errorf("entities = %q, want %q", got, want)
	}
}

// TestSemanticCorpusLanguageFiltering: verifies "unknown", empty, or whitespace language
// values are ignored, while valid language codes are preserved in the result map.
func TestSemanticCorpusLanguageFiltering(t *testing.T) {
	testCases := []struct {
		name       string
		corpusLang string
		wantLang   string
		hasLangKey bool
	}{
		{
			name:       "valid language",
			corpusLang: "en",
			wantLang:   "en",
			hasLangKey: true,
		},
		{
			name:       "unknown language",
			corpusLang: "unknown",
			wantLang:   "",
			hasLangKey: false,
		},
		{
			name:       "empty language",
			corpusLang: "",
			wantLang:   "",
			hasLangKey: false,
		},
		{
			name:       "whitespace language",
			corpusLang: "   ",
			wantLang:   "",
			hasLangKey: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/health":
					w.Write([]byte(`{"status":"ok","version":"0.2.0","models":{"lid":true,"ner":true,"keyphrase":true,"topic":true}}`))
				case "/tag":
					out := map[string]any{
						"results": []map[string]any{
							{"id": 0, "tags": []string{"tag1"}},
						},
						"errors":      []any{},
						"corpus_lang": tc.corpusLang,
					}
					w.Write(mustJSON(t, out))
				}
			}))
			defer srv.Close()

			cfg := cfgFromTest(t, tmp, filepath.Join(tmp, "test.db"))
			cfg.Config.Semantic.Enabled = true
			cfg.Config.Semantic.BaseURL = srv.URL

			idx := New(cfg, nil)
			idx.WithLogger(nopLogger{})

			fields := idx.semanticFastFields(context.Background(), "doc.md", textChunks("sample text"))
			if fields == nil {
				t.Fatal("expected non-nil fields")
			}

			lang, exists := fields["language"]
			if exists != tc.hasLangKey {
				t.Errorf("language key exists = %v, want %v", exists, tc.hasLangKey)
			}
			if lang != tc.wantLang {
				t.Errorf("language = %q, want %q", lang, tc.wantLang)
			}
		})
	}
}

// TestSemanticEmptyResultsContract: verifies that when the service succeeds but produces
// no tags, topics, entities, or corpus language, an empty non-nil map (map[string]string{})
// is returned (contract: successful recompute to nothing).
func TestSemanticEmptyResultsContract(t *testing.T) {
	tmp := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.Write([]byte(`{"status":"ok","version":"0.2.0","models":{"lid":true,"ner":true,"keyphrase":true,"topic":true}}`))
		case "/tag":
			out := map[string]any{
				"results": []map[string]any{
					{
						"id":       0,
						"tags":     []string{},
						"topics":   []map[string]any{},
						"entities": []map[string]any{},
					},
				},
				"errors":      []any{},
				"corpus_lang": "",
			}
			w.Write(mustJSON(t, out))
		}
	}))
	defer srv.Close()

	cfg := cfgFromTest(t, tmp, filepath.Join(tmp, "test.db"))
	cfg.Config.Semantic.Enabled = true
	cfg.Config.Semantic.BaseURL = srv.URL

	idx := New(cfg, nil)
	idx.WithLogger(nopLogger{})

	fields := idx.semanticFastFields(context.Background(), "doc.md", textChunks("sample text"))
	if fields == nil {
		t.Fatal("semanticFastFields returned nil, want non-nil empty map map[string]string{}")
	}
	if len(fields) != 0 {
		t.Errorf("len(fields) = %d, want 0", len(fields))
	}
}

// TestSemanticProviderCachingAndCapabilities: tests that semanticProvider caches the client,
// logs health capabilities, and handles semChecked correctly.
func TestSemanticProviderCachingAndCapabilities(t *testing.T) {
	tmp := t.TempDir()
	healthCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			healthCalls++
			w.Write([]byte(`{"status":"ok","version":"0.2.0","models":{"lid":true,"ner":true,"keyphrase":true,"topic":true}}`))
		}
	}))
	defer srv.Close()

	cfg := cfgFromTest(t, tmp, filepath.Join(tmp, "test.db"))
	cfg.Config.Semantic.Enabled = true
	cfg.Config.Semantic.BaseURL = srv.URL

	var logOutput string
	idx := New(cfg, nil)
	idx.WithLogger(captureLogger{buf: &logOutput})

	// First call to semanticProvider resolves health and caches client.
	p1 := idx.semanticProvider()
	if p1 == nil {
		t.Fatal("expected non-nil provider on first call")
	}

	// Second call should return cached provider without making another /health call.
	p2 := idx.semanticProvider()
	if p2 == nil {
		t.Fatal("expected non-nil provider on second call")
	}

	if p1 != p2 {
		t.Errorf("semanticProvider returned different instances; want cached instance")
	}

	if healthCalls != 1 {
		t.Errorf("healthCalls = %d, want 1 (cached after first call)", healthCalls)
	}

	if !idx.semChecked {
		t.Errorf("semChecked = false, want true")
	}

	// Verify capability logging output
	for _, capStr := range []string{"lid", "ner", "keyphrase", "topic"} {
		if !strings.Contains(logOutput, capStr) {
			t.Errorf("log output missing capability %q: %q", capStr, logOutput)
		}
	}
}

// TestSemanticProviderOfflineValidationFailure: tests that offline validation failure disables provider.
func TestSemanticProviderOfflineValidationFailure(t *testing.T) {
	tmp := t.TempDir()
	cfg := cfgFromTest(t, tmp, filepath.Join(tmp, "test.db"))
	cfg.Config.Semantic.Enabled = true
	cfg.Config.Semantic.BaseURL = "http://external-server.com:8000"
	cfg.Config.Privacy.OfflineOnly = true

	var warned string
	idx := New(cfg, nil)
	idx.WithLogger(captureLogger{buf: &warned})

	p := idx.semanticProvider()
	if p != nil {
		t.Errorf("expected nil provider when offline validation fails, got %v", p)
	}
	if !strings.Contains(warned, "offline_only") {
		t.Errorf("expected offline_only in warning log, got %q", warned)
	}
}

// TestSemanticProviderDisabledConfig: tests that when Config.Semantic.Enabled is false, semanticProvider returns nil.
func TestSemanticProviderDisabledConfig(t *testing.T) {
	tmp := t.TempDir()
	cfg := cfgFromTest(t, tmp, filepath.Join(tmp, "test.db"))
	cfg.Config.Semantic.Enabled = false

	idx := New(cfg, nil)
	idx.WithLogger(nopLogger{})

	if p := idx.semanticProvider(); p != nil {
		t.Errorf("expected nil provider when semantic disabled, got %v", p)
	}
}

// TestSemanticProviderNilConfig: tests that when idx.cfg is nil, semanticProvider returns nil.
func TestSemanticProviderNilConfig(t *testing.T) {
	idx := New(nil, nil)
	idx.WithLogger(nopLogger{})

	if p := idx.semanticProvider(); p != nil {
		t.Errorf("expected nil provider when idx.cfg is nil, got %v", p)
	}
}

// Verify mock implementation compliance with semantic.Provider interface.
var _ semantic.Provider = (*semantic.Client)(nil)
