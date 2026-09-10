package indexer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ozgurulukir/seek/internal/store"
)

// startFakeSemantic returns a fake semantic service (contract v0.2.0) and
// its URL. It returns the same tags/topics/entities for every chunk and a
// fixed corpus_lang.
func startFakeSemantic(t *testing.T, tags []string, corpusLang string) *httptest.Server {
	t.Helper()
	type topicT struct {
		Label string  `json:"label"`
		Score float64 `json:"score"`
	}
	type entityT struct {
		Text string `json:"text"`
		Type string `json:"type"`
	}
	topicList := []topicT{{Label: "go concurrency", Score: 0.9}}
	entityList := []entityT{{Text: "Go", Type: "LOC"}, {Text: "OpenAI", Type: "MISC"}}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			type result struct {
				ID       int       `json:"id"`
				Tags     []string  `json:"tags"`
				Topics   []topicT  `json:"topics"`
				Entities []entityT `json:"entities"`
			}
			out := struct {
				Results    []result `json:"results"`
				Errors     []any    `json:"errors"`
				CorpusLang string   `json:"corpus_lang"`
			}{Errors: []any{}, CorpusLang: corpusLang}
			for _, c := range req.Chunks {
				out.Results = append(out.Results, result{ID: c.ID, Tags: tags, Topics: topicList, Entities: entityList})
			}
			w.Write(mustJSON(t, out))
		default:
			t.Errorf("fake semantic: unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

// textChunks wraps a single text as the chunk list passed to semanticFastFields.
func textChunks(text string) []store.IndexChunk {
	return []store.IndexChunk{{Seq: 0, Content: text}}
}

// TestSemanticFastFieldsFromService: enabled service → all four fields.
func TestSemanticFastFieldsFromService(t *testing.T) {
	tmp := t.TempDir()
	srv := startFakeSemantic(t, []string{"golang", "concurrency", "golang"}, "en")
	defer srv.Close()

	cfg := cfgFromTest(t, tmp, filepath.Join(tmp, "test.db"))
	cfg.Config.Semantic.Enabled = true
	cfg.Config.Semantic.BaseURL = srv.URL

	idx := New(cfg, nil)
	idx.WithLogger(nopLogger{})

	fields := idx.semanticFastFields(context.Background(), "test.pdf", textChunks("some text about go concurrency and channels"))
	if fields == nil {
		t.Fatal("semanticFastFields returned nil; want fields from fake service")
	}
	if got := fields["tags"]; got != "golang,concurrency" {
		t.Errorf("tags = %q, want golang,concurrency (deduped)", got)
	}
	if got := fields["topics"]; got != "go concurrency" {
		t.Errorf("topics = %q, want go concurrency", got)
	}
	if got := fields["entities"]; got != "LOC:Go,MISC:OpenAI" {
		t.Errorf("entities = %q, want LOC:Go,MISC:OpenAI", got)
	}
	if got := fields["language"]; got != "en" {
		t.Errorf("language = %q, want en", got)
	}
}

// TestSemanticFastFieldsDegrade: service down → nil fields, no panic.
func TestSemanticFastFieldsDegrade(t *testing.T) {
	tmp := t.TempDir()
	cfg := cfgFromTest(t, tmp, filepath.Join(tmp, "test.db"))
	cfg.Config.Semantic.Enabled = true
	cfg.Config.Semantic.BaseURL = "http://127.0.0.1:1" // closed port

	idx := New(cfg, nil)
	idx.WithLogger(nopLogger{})

	if fields := idx.semanticFastFields(context.Background(), "x.pdf", textChunks("text")); fields != nil {
		t.Fatalf("want nil fields when service is down, got %v", fields)
	}
	// Provider is cached as unavailable: second call must not re-check and
	// must stay nil.
	if fields := idx.semanticFastFields(context.Background(), "y.pdf", textChunks("text")); fields != nil {
		t.Fatalf("second call after degradation must be nil, got %v", fields)
	}
}

// TestSemanticDisabled: capability off → no fields, no HTTP attempted.
func TestSemanticDisabled(t *testing.T) {
	tmp := t.TempDir()
	cfg := cfgFromTest(t, tmp, filepath.Join(tmp, "test.db"))
	idx := New(cfg, nil)
	idx.WithLogger(nopLogger{})
	if fields := idx.semanticFastFields(context.Background(), "x.pdf", textChunks("text")); fields != nil {
		t.Fatalf("want nil fields when semantic disabled, got %v", fields)
	}
}

// TestSemanticOfflineGate: offline_only + non-loopback → refused.
func TestSemanticOfflineGate(t *testing.T) {
	tmp := t.TempDir()
	cfg := cfgFromTest(t, tmp, filepath.Join(tmp, "test.db"))
	cfg.Config.Semantic.Enabled = true
	cfg.Config.Semantic.BaseURL = "http://nlp.example.com:8003"
	cfg.Config.Privacy.OfflineOnly = true

	idx := New(cfg, nil)
	var warned string
	idx.WithLogger(captureLogger{buf: &warned})

	if fields := idx.semanticFastFields(context.Background(), "x.pdf", textChunks("text")); fields != nil {
		t.Fatalf("want nil fields under offline gate, got %v", fields)
	}
	if !strings.Contains(warned, "offline_only") {
		t.Fatalf("want offline gate warning, got %q", warned)
	}
}

// TestSyncPdfWritesSemanticFastFields: end-to-end through the real store
// writer — a synced PDF gains all four fast fields when the service is up.
func TestSyncPdfWritesSemanticFastFields(t *testing.T) {
	tmp := t.TempDir()
	db, err := store.Open(filepath.Join(tmp, "test.db"))
	if err != nil {
		if strings.Contains(err.Error(), "SQLite FTS5 not enabled") {
			t.Skip("SQLite FTS5 not enabled")
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	srv := startFakeSemantic(t, []string{"finance", "2024-report"}, "en")
	defer srv.Close()

	cfg := cfgFromTest(t, tmp, filepath.Join(tmp, "test.db"))
	cfg.Config.Semantic.Enabled = true
	cfg.Config.Semantic.BaseURL = srv.URL

	col, err := db.CreateCollection("papers", store.CollectionTypePDF, tmp, "*.pdf")
	if err != nil {
		t.Fatal(err)
	}
	// A real minimal PDF with an embedded text layer so the extractor path
	// runs without OCR.
	pdfPath := filepath.Join(tmp, "report.pdf")
	writeMinimalPDF(t, pdfPath)

	idx := New(cfg, db)
	idx.WithLogger(nopLogger{})
	if err := idx.SyncCollectionContext(context.Background(), col); err != nil {
		t.Fatal(err)
	}

	doc, err := db.GetDocumentContext(context.Background(), col.ID, pdfPath)
	if err != nil {
		t.Fatal(err)
	}
	for field, want := range map[string]string{
		"tags":     "finance,2024-report",
		"topics":   "go concurrency",
		"entities": "LOC:Go,MISC:OpenAI",
		"language": "en",
	} {
		gotVal, err := db.FastFields().Get(doc.ID, field)
		if err != nil {
			t.Fatalf("Get(%q): %v", field, err)
		}
		got, _ := gotVal.(string)
		if got != want {
			t.Errorf("fast field %q = %q, want %q", field, got, want)
		}
	}
}

// captureLogger records output for assertions.
type captureLogger struct{ buf *string }

func (c captureLogger) Printf(format string, v ...interface{}) {
	*c.buf += fmt.Sprintf(format, v...)
}

// writeMinimalPDF writes a small valid one-page PDF with an embedded text
// layer so the extractor path runs without OCR. Offsets are computed so the
// xref table is correct.
func writeMinimalPDF(t *testing.T, path string) {
	t.Helper()
	var objs []string
	objs = append(objs,
		`<< /Type /Catalog /Pages 2 0 R >>`,
		`<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>`,
	)
	stream := "BT /F1 24 Tf 72 720 Td (Hello semantic test) Tj ET"
	objs = append(objs, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream))
	objs = append(objs, `<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`)

	var buf strings.Builder
	buf.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objs))
	for i, body := range objs {
		offsets[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	xrefStart := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n", len(objs)+1)
	buf.WriteString("0000000000 65535 f \n")
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xrefStart)

	if err := os.WriteFile(path, []byte(buf.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}
