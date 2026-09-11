package cmd_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ozgurulukir/seek/cmd"
	"github.com/ozgurulukir/seek/internal/config"
)

func TestFieldsCmd_EmptyDatabase(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db := openTestStore(t, dbPath)
	db.Close()

	cfg := &config.AppConfig{DBPath: dbPath}

	// 1. Summary on empty database
	fieldsCmd := &cmd.FieldsCmd{}
	var runErr error
	out := captureStdout(t, func() {
		runErr = fieldsCmd.Run(cfg)
	})
	if runErr != nil {
		t.Fatalf("FieldsCmd.Run failed: %v", runErr)
	}
	if !strings.Contains(out, "No documents found") {
		t.Errorf("expected 'No documents found', got: %s", out)
	}

	// 2. Summary in JSON on empty database
	fieldsCmdJSON := &cmd.FieldsCmd{JSON: true}
	var runErrJSON error
	outJSON := captureStdout(t, func() {
		runErrJSON = fieldsCmdJSON.Run(cfg)
	})
	if runErrJSON != nil {
		t.Fatalf("FieldsCmd.Run JSON failed: %v", runErrJSON)
	}
	var resp struct {
		TotalDocs int `json:"total_docs"`
	}
	if err := json.Unmarshal([]byte(outJSON), &resp); err != nil {
		t.Fatalf("unmarshal json: %v (raw: %s)", err, outJSON)
	}
	if resp.TotalDocs != 0 {
		t.Errorf("expected total_docs=0, got %d", resp.TotalDocs)
	}
}

func TestFieldsCmd_WithData(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db := openTestStore(t, dbPath)

	col1, err := db.CreateCollection("notes", "markdown", "/notes", "**/*.md")
	if err != nil {
		t.Fatal(err)
	}
	col2, err := db.CreateCollection("code", "code", "/code", "**/*.go")
	if err != nil {
		t.Fatal(err)
	}

	d1, err := db.UpsertDocument(col1.ID, "/notes/a.md", "a.md", "h1", 1, 50)
	if err != nil {
		t.Fatal(err)
	}
	_ = db.FastFields().Set(d1, "tags", "go,concurrency")
	_ = db.FastFields().Set(d1, "language", "en")

	d2, err := db.UpsertDocument(col1.ID, "/notes/b.md", "b.md", "h2", 1, 80)
	if err != nil {
		t.Fatal(err)
	}
	_ = db.FastFields().Set(d2, "tags", "go,sqlite")
	_ = db.FastFields().Set(d2, "language", "en")

	d3, err := db.UpsertDocument(col2.ID, "/code/c.go", "c.go", "h3", 1, 120)
	if err != nil {
		t.Fatal(err)
	}
	_ = db.FastFields().Set(d3, "tags", "rust,concurrency")
	_ = db.FastFields().Set(d3, "language", "tr")

	db.Close()
	cfg := &config.AppConfig{DBPath: dbPath}

	// 1. Table summary
	fieldsCmd := &cmd.FieldsCmd{}
	var sumErr error
	out := captureStdout(t, func() {
		sumErr = fieldsCmd.Run(cfg)
	})
	if sumErr != nil {
		t.Fatalf("summary error: %v", sumErr)
	}
	if !strings.Contains(out, "FIELD") || !strings.Contains(out, "tags") || !strings.Contains(out, "membership") {
		t.Errorf("expected table header and tags membership, got: %s", out)
	}

	// 2. JSON summary
	fieldsCmdJSON := &cmd.FieldsCmd{JSON: true}
	var sumErrJSON error
	outJSON := captureStdout(t, func() {
		sumErrJSON = fieldsCmdJSON.Run(cfg)
	})
	if sumErrJSON != nil {
		t.Fatalf("summary JSON error: %v", sumErrJSON)
	}
	var sumResp struct {
		TotalDocs int `json:"total_docs"`
		Fields    []struct {
			FieldName      string  `json:"field_name"`
			DistinctValues int     `json:"distinct_values"`
			DocCount       int     `json:"doc_count"`
			Coverage       float64 `json:"coverage_percent"`
		} `json:"fields"`
	}
	if err := json.Unmarshal([]byte(outJSON), &sumResp); err != nil {
		t.Fatalf("json parse error: %v, raw: %s", err, outJSON)
	}
	if sumResp.TotalDocs != 3 {
		t.Errorf("expected 3 total docs, got %d", sumResp.TotalDocs)
	}

	// 3. Field values: tags (membership token splitting)
	tagsCmd := &cmd.FieldsCmd{Name: "tags"}
	var tagsErr error
	outTags := captureStdout(t, func() {
		tagsErr = tagsCmd.Run(cfg)
	})
	if tagsErr != nil {
		t.Fatalf("tags values error: %v", tagsErr)
	}
	if !strings.Contains(outTags, "go") || !strings.Contains(outTags, "concurrency") {
		t.Errorf("expected split tokens 'go' and 'concurrency', got:\n%s", outTags)
	}

	// 4. Field values in JSON: tags
	tagsJSONCmd := &cmd.FieldsCmd{Name: "tags", JSON: true}
	var tagsJSONErr error
	outTagsJSON := captureStdout(t, func() {
		tagsJSONErr = tagsJSONCmd.Run(cfg)
	})
	if tagsJSONErr != nil {
		t.Fatalf("tags JSON error: %v", tagsJSONErr)
	}
	var tagItems []struct {
		Value string `json:"value"`
		Count int    `json:"count"`
	}
	if err := json.Unmarshal([]byte(outTagsJSON), &tagItems); err != nil {
		t.Fatalf("parse tag items json: %v, raw: %s", err, outTagsJSON)
	}
	if len(tagItems) != 4 { // go, concurrency, sqlite, rust
		t.Fatalf("expected 4 distinct tag items, got %d: %#v", len(tagItems), tagItems)
	}
	// 'go' and 'concurrency' each appear in 2 documents
	if tagItems[0].Count != 2 || tagItems[1].Count != 2 {
		t.Errorf("expected top 2 items to have count 2, got: %#v", tagItems)
	}

	// 5. Prefix filtering: prefix "con"
	prefixCmd := &cmd.FieldsCmd{Name: "tags", Prefix: "con", JSON: true}
	var prefixErr error
	outPrefix := captureStdout(t, func() {
		prefixErr = prefixCmd.Run(cfg)
	})
	if prefixErr != nil {
		t.Fatalf("prefix error: %v", prefixErr)
	}
	var prefixItems []struct {
		Value string `json:"value"`
		Count int    `json:"count"`
	}
	if err := json.Unmarshal([]byte(outPrefix), &prefixItems); err != nil {
		t.Fatal(err)
	}
	if len(prefixItems) != 1 || prefixItems[0].Value != "concurrency" {
		t.Errorf("expected [concurrency], got: %#v", prefixItems)
	}

	// 6. Unknown field error
	badCmd := &cmd.FieldsCmd{Name: "unknown_field"}
	err = badCmd.Run(cfg)
	if err == nil {
		t.Fatal("expected error for unknown fast field")
	}
	if !strings.Contains(err.Error(), "unknown fast field") {
		t.Errorf("unexpected error: %v", err)
	}
}
