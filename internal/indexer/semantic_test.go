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

// startFakeSemantic returns a fake semantic service and its URL.
func startFakeSemantic(t *testing.T, tags []string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.Write([]byte(`{"status":"ok","version":"0.1.0","models":{"lid":true,"ner":true,"keyphrase":true,"topic":true}}`))
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
				ID   int      `json:"id"`
				Tags []string `json:"tags"`
			}
			out := struct {
				Results []result `json:"results"`
				Errors  []any    `json:"errors"`
			}{Errors: []any{}}
			for _, c := range req.Chunks {
				out.Results = append(out.Results, result{ID: c.ID, Tags: tags})
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

// TestSemanticTagsFromService: enabled service → tags come back, merged.
func TestSemanticTagsFromService(t *testing.T) {
	tmp := t.TempDir()
	srv := startFakeSemantic(t, []string{"golang", "concurrency", "golang"})
	defer srv.Close()

	cfg := cfgFromTest(t, tmp, filepath.Join(tmp, "test.db"))
	cfg.Config.Semantic.Enabled = true
	cfg.Config.Semantic.BaseURL = srv.URL

	idx := New(cfg, nil)
	idx.WithLogger(nopLogger{})

	tags := idx.semanticTags(context.Background(), "test.pdf", "some text about go concurrency and channels")
	if tags == nil {
		t.Fatal("semanticTags returned nil; want tags from fake service")
	}
	// Duplicated tag across chunks must be deduped.
	seen := map[string]bool{}
	for _, tag := range tags {
		if seen[tag] {
			t.Fatalf("duplicate tag %q in %v", tag, tags)
		}
		seen[tag] = true
	}
	if !seen["golang"] || !seen["concurrency"] {
		t.Fatalf("want golang+concurrency tags, got %v", tags)
	}
}

// TestSemanticTagsDegrade: service down → nil tags, no panic, keyword-safe.
func TestSemanticTagsDegrade(t *testing.T) {
	tmp := t.TempDir()
	cfg := cfgFromTest(t, tmp, filepath.Join(tmp, "test.db"))
	cfg.Config.Semantic.Enabled = true
	cfg.Config.Semantic.BaseURL = "http://127.0.0.1:1" // closed port

	idx := New(cfg, nil)
	idx.WithLogger(nopLogger{})

	if tags := idx.semanticTags(context.Background(), "x.pdf", "text"); tags != nil {
		t.Fatalf("want nil tags when service is down, got %v", tags)
	}
	// Provider is cached as unavailable: second call must not re-check and
	// must stay nil.
	if tags := idx.semanticTags(context.Background(), "y.pdf", "text"); tags != nil {
		t.Fatalf("second call after degradation must be nil, got %v", tags)
	}
}

// TestSemanticDisabled: capability off → provider nil → no tags, no
// health check attempted (the fake server must not be hit).
func TestSemanticDisabled(t *testing.T) {
	tmp := t.TempDir()
	cfg := cfgFromTest(t, tmp, filepath.Join(tmp, "test.db"))
	// Enabled=false → semanticProvider must short-circuit before any HTTP.
	idx := New(cfg, nil)
	idx.WithLogger(nopLogger{})
	if tags := idx.semanticTags(context.Background(), "x.pdf", "text"); tags != nil {
		t.Fatalf("want nil tags when semantic disabled, got %v", tags)
	}
}

// TestSemanticOfflineGate: offline_only + non-loopback → refused, no HTTP.
func TestSemanticOfflineGate(t *testing.T) {
	tmp := t.TempDir()
	cfg := cfgFromTest(t, tmp, filepath.Join(tmp, "test.db"))
	cfg.Config.Semantic.Enabled = true
	cfg.Config.Semantic.BaseURL = "http://nlp.example.com:8003"
	cfg.Config.Privacy.OfflineOnly = true

	idx := New(cfg, nil)
	var warned string
	idx.WithLogger(captureLogger{buf: &warned})

	if tags := idx.semanticTags(context.Background(), "x.pdf", "text"); tags != nil {
		t.Fatalf("want nil tags under offline gate, got %v", tags)
	}
	if !strings.Contains(warned, "offline_only") {
		t.Fatalf("want offline gate warning, got %q", warned)
	}
}

// TestSemanticTagMap encoding matches markdown frontmatter (comma-joined).
func TestSemanticTagMap(t *testing.T) {
	m := semanticTagMap([]string{"go", "rust"})
	if m == nil || m["tags"] != "go,rust" {
		t.Fatalf("want tags=go,rust, got %v", m)
	}
	if semanticTagMap(nil) != nil {
		t.Fatal("empty tags must map to nil fast fields")
	}
}

// TestSyncPdfWritesSemanticTags: end-to-end through the real store writer —
// a synced PDF ends up with a `tags` fast field when the service is up.
func TestSyncPdfWritesSemanticTags(t *testing.T) {
	tmp := t.TempDir()
	db, err := store.Open(filepath.Join(tmp, "test.db"))
	if err != nil {
		if strings.Contains(err.Error(), "SQLite FTS5 not enabled") {
			t.Skip("SQLite FTS5 not enabled")
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	srv := startFakeSemantic(t, []string{"finance", "2024-report"})
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
	gotVal, err := db.FastFields().Get(doc.ID, "tags")
	if err != nil {
		t.Fatalf("Get(tags): %v", err)
	}
	got, _ := gotVal.(string)
	if got != "finance,2024-report" {
		t.Fatalf("want tags fast field finance,2024-report, got %q", got)
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
