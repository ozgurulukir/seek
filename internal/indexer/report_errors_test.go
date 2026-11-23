package indexer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/store"
)

// TestFileSyncPersistenceFailuresRecordedInReport pins the A1-micro audit
// fix: every file handler routes a per-file persistence failure through
// recordFailure, so the machine-readable SyncReport.Errors carries the
// failing path for markdown, code, image, and PDF syncs too — not only the
// documents handler. Before this, four of the five handlers only warned and
// bumped the failed counter, losing the path from the JSON report.
func TestFileSyncPersistenceFailuresRecordedInReport(t *testing.T) {
	tests := []struct {
		name   string
		addCol func(t *testing.T, db *store.Store, root string) *store.Collection
		// wantKind defaults to "persistence"; PDF fails earlier, at extract.
		wantKind        string
		skipErrContains bool
	}{
		{
			name: "markdown",
			addCol: func(t *testing.T, db *store.Store, root string) *store.Collection {
				if err := os.WriteFile(filepath.Join(root, "note.md"), []byte("# Title\n\nbody text\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				col, err := db.CreateCollection("notes", store.CollectionTypeMarkdown, root, "*.md")
				if err != nil {
					t.Fatal(err)
				}
				return col
			},
		},
		{
			name: "code",
			addCol: func(t *testing.T, db *store.Store, root string) *store.Collection {
				if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				col, err := db.CreateCollection("code", store.CollectionTypeCode, root, "*.go")
				if err != nil {
					t.Fatal(err)
				}
				return col
			},
		},
		{
			name: "image",
			addCol: func(t *testing.T, db *store.Store, root string) *store.Collection {
				// A 1x1 PNG so the image scanner accepts the file.
				png := []byte{
					0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D,
					0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
					0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4, 0x89, 0x00, 0x00, 0x00,
					0x0D, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00,
					0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00, 0x00, 0x00, 0x00, 0x49,
					0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82,
				}
				if err := os.WriteFile(filepath.Join(root, "pic.png"), png, 0o644); err != nil {
					t.Fatal(err)
				}
				col, err := db.CreateCollection("pics", store.CollectionTypeImages, root, "*.png")
				if err != nil {
					t.Fatal(err)
				}
				return col
			},
		},
		{
			name: "claude conversation",
			addCol: func(t *testing.T, db *store.Store, root string) *store.Collection {
				// Claude scans ~/.claude/projects; point the home dir at the
				// temp root so the scan sees a real JSONL session.
				line := `{"type":"user","message":{"role":"user","content":"Hello there"}}` + "\n"
				proj := filepath.Join(root, ".claude", "projects", "proj")
				if err := os.MkdirAll(proj, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(proj, "session.jsonl"), []byte(line), 0o644); err != nil {
					t.Fatal(err)
				}
				t.Setenv("HOME", root)
				t.Setenv("USERPROFILE", root)
				col, err := db.CreateCollection("conv", store.CollectionTypeClaude, root, "*.jsonl")
				if err != nil {
					t.Fatal(err)
				}
				return col
			},
		},
		{
			name: "pdf",
			addCol: func(t *testing.T, db *store.Store, root string) *store.Collection {
				// Garbage bytes: the PDF scanner accepts the extension, the
				// extractor fails, and the handler must record the failure.
				if err := os.WriteFile(filepath.Join(root, "broken.pdf"), []byte("not a pdf at all\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				col, err := db.CreateCollection("docs", store.CollectionTypePDF, root, "*.pdf")
				if err != nil {
					t.Fatal(err)
				}
				return col
			},
			// The extractor's message for garbage input is an implementation
			// detail; only the kind and path are pinned here.
			wantKind:        "extraction",
			skipErrContains: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
			if err != nil {
				if strings.Contains(err.Error(), "SQLite FTS5 not enabled") {
					t.Skip("SQLite FTS5 not enabled. Run tests with: go test -tags \"fts5 sqlite_fts5\" ./... or make test")
				}
				t.Fatal(err)
			}
			t.Cleanup(func() { db.Close() })

			col := tt.addCol(t, db, root)
			cfg := &config.AppConfig{Config: config.Config{}, CacheDir: t.TempDir(), DBPath: filepath.Join(t.TempDir(), "x.db")}

			boom := errors.New("boom: writer unavailable")
			writer := &fakeIndexWriter{
				upsertAndReplaceFunc: func(ctx context.Context, doc store.DocumentIndex) (int64, error) {
					return 0, boom
				},
			}
			idx := New(cfg, db).WithLogger(nopLogger{}).WithIndexWriter(writer)

			if err := idx.SyncCollection(col); err != nil {
				t.Fatalf("SyncCollection: %v", err)
			}

			report := idx.LastReport()
			if report.Failed != 1 {
				t.Fatalf("report.Failed = %d, want 1", report.Failed)
			}
			if len(report.Errors) != 1 {
				t.Fatalf("report.Errors = %+v, want exactly one persistence entry", report.Errors)
			}
			got := report.Errors[0]
			wantKind := tt.wantKind
			if wantKind == "" {
				wantKind = "persistence"
			}
			if got.Kind != wantKind {
				t.Errorf("failure kind = %q, want %s", got.Kind, wantKind)
			}
			if got.Path == "" {
				t.Error("failure path is empty; the report lost the failing file")
			}
			if got.Error == "" {
				t.Error("failure error text is empty")
			}
			if tt.skipErrContains {
				return
			}
			if !strings.Contains(got.Error, boom.Error()) {
				// code's indexCodeFile wraps the write error with its own
				// context, so assert on the cause rather than the full text.
				t.Errorf("failure error = %q, want it to contain %q", got.Error, boom.Error())
			}
		})
	}
}
