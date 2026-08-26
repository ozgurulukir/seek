package embed

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

// newTestBatchClient returns a Client whose HTTP transport points at the given
// handler, for exercising the batch API contract.
func newTestBatchClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := NewClient(srv.URL, "test-key", "test-model", 8, TaskPrefix{})
	c.http = srv.Client()
	return c
}

func TestPrepareBatchJSONL(t *testing.T) {
	c := NewClient("https://api.example/v1", "k", "m", 4, TaskPrefix{})
	data, err := c.PrepareBatchJSONL([]string{"hello", "world"})
	if err != nil {
		t.Fatalf("PrepareBatchJSONL: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}

	var first batchRequestLine
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("unmarshal line 0: %v", err)
	}
	if first.CustomID != "chunk-0" {
		t.Errorf("custom_id = %q, want chunk-0", first.CustomID)
	}
	if first.Method != "POST" {
		t.Errorf("method = %q, want POST", first.Method)
	}
	if first.URL != "/v1/embeddings" {
		t.Errorf("url = %q, want /v1/embeddings", first.URL)
	}
	if first.Body["model"] != "m" {
		t.Errorf("body.model = %v, want m", first.Body["model"])
	}
	if first.Body["input"] != "hello" {
		t.Errorf("body.input = %v, want hello", first.Body["input"])
	}
	if first.Body["dimensions"] != float64(4) {
		t.Errorf("body.dimensions = %v, want 4", first.Body["dimensions"])
	}
}

func TestPrepareBatchJSONLOmitsDimensionsWhenZero(t *testing.T) {
	c := NewClient("https://api.example/v1", "k", "m", 0, TaskPrefix{})
	data, err := c.PrepareBatchJSONL([]string{"x"})
	if err != nil {
		t.Fatalf("PrepareBatchJSONL: %v", err)
	}
	var line batchRequestLine
	if err := json.Unmarshal(data, &line); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := line.Body["dimensions"]; ok {
		t.Error("dimensions should be omitted when zero")
	}
}

func TestUploadBatchFile(t *testing.T) {
	var gotAuth string
	var gotPurpose string
	var gotFileContent string

	c := newTestBatchClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/files" {
			t.Errorf("path = %q, want /files", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("parse multipart: %v", err)
		}
		gotPurpose = r.FormValue("purpose")
		file, _, err := r.FormFile("file")
		if err != nil {
			t.Errorf("form file: %v", err)
			return
		}
		defer file.Close()
		buf := make([]byte, 1024)
		n, _ := file.Read(buf)
		gotFileContent = string(buf[:n])

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": "file-123"})
	})

	id, err := c.UploadBatchFile([]byte("{\"custom_id\":\"chunk-0\"}\n"))
	if err != nil {
		t.Fatalf("UploadBatchFile: %v", err)
	}
	if id != "file-123" {
		t.Errorf("id = %q, want file-123", id)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("Authorization = %q, want Bearer test-key", gotAuth)
	}
	if gotPurpose != "batch" {
		t.Errorf("purpose = %q, want batch", gotPurpose)
	}
	if !strings.Contains(gotFileContent, "chunk-0") {
		t.Errorf("file content = %q, want chunk-0", gotFileContent)
	}
}

func TestUploadBatchFileError(t *testing.T) {
	c := newTestBatchClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("nope"))
	})
	if _, err := c.UploadBatchFile([]byte("{}")); err == nil {
		t.Fatal("expected error on non-200 upload")
	}
}

func TestCreateBatch(t *testing.T) {
	var gotPath string
	var gotBody map[string]interface{}

	c := newTestBatchClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": "batch-1", "status": "queued"})
	})

	job, err := c.CreateBatch("file-123")
	if err != nil {
		t.Fatalf("CreateBatch: %v", err)
	}
	if gotPath != "/batches" {
		t.Errorf("path = %q, want /batches", gotPath)
	}
	if gotBody["input_file_id"] != "file-123" {
		t.Errorf("input_file_id = %v, want file-123", gotBody["input_file_id"])
	}
	if gotBody["endpoint"] != "/v1/embeddings" {
		t.Errorf("endpoint = %v, want /v1/embeddings", gotBody["endpoint"])
	}
	if job.ID != "batch-1" || job.Status != "queued" {
		t.Errorf("job = %+v, want id=batch-1 status=queued", job)
	}
}

func TestPollBatchCompletes(t *testing.T) {
	var polls int
	c := newTestBatchClient(t, func(w http.ResponseWriter, r *http.Request) {
		polls++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"id":             "batch-1",
			"status":         "completed",
			"output_file_id": "file-out",
		})
	})

	var statuses []string
	job, err := c.PollBatch("batch-1", func(status string, _ time.Duration) {
		statuses = append(statuses, status)
	})
	if err != nil {
		t.Fatalf("PollBatch: %v", err)
	}
	if polls != 1 {
		t.Errorf("polls = %d, want 1", polls)
	}
	if job.Status != "completed" || job.OutputFileID != "file-out" {
		t.Errorf("job = %+v, want completed with output file", job)
	}
	if !reflect.DeepEqual(statuses, []string{"completed"}) {
		t.Errorf("statuses = %v, want [completed]", statuses)
	}
}

func TestPollBatchFailed(t *testing.T) {
	c := newTestBatchClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": "batch-1", "status": "failed"})
	})

	_, err := c.PollBatch("batch-1", nil)
	if err == nil {
		t.Fatal("expected error for failed batch")
	}
	if !strings.Contains(err.Error(), "failed") {
		t.Errorf("error = %q, want mention of failed", err.Error())
	}
}

func TestDownloadBatchResults(t *testing.T) {
	// Two valid lines plus one error line and one non-200 line that must be skipped.
	body := `{"custom_id":"chunk-0","response":{"status_code":200,"body":{"data":[{"embedding":[1,2],"index":0}]}}}
{"custom_id":"chunk-1","response":{"status_code":200,"body":{"data":[{"embedding":[3,4],"index":0}]}}}
{"custom_id":"chunk-2","response":{"status_code":500,"body":{}}}
{"custom_id":"chunk-3","error":{"code":"x","message":"boom"}}
`

	c := newTestBatchClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/files/file-out/content" {
			t.Errorf("path = %q, want /files/file-out/content", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/jsonl")
		w.Write([]byte(body))
	})

	got, err := c.DownloadBatchResults("file-out")
	if err != nil {
		t.Fatalf("DownloadBatchResults: %v", err)
	}
	want := map[string][]float32{
		"chunk-0": {1, 2},
		"chunk-1": {3, 4},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("results = %v, want %v", got, want)
	}
}

func TestDownloadBatchResultsError(t *testing.T) {
	c := newTestBatchClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("missing"))
	})
	if _, err := c.DownloadBatchResults("file-out"); err == nil {
		t.Fatal("expected error on non-200 download")
	}
}

// TestBatchEmbedAsync exercises the full pipeline against a stateful mock
// server that walks the job through queued -> completed.
func TestBatchEmbedAsync(t *testing.T) {
	var uploads, creates, polls, downloads int

	c := newTestBatchClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && r.URL.Path == "/files":
			uploads++
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"id": "file-1"})

		case r.Method == "POST" && r.URL.Path == "/batches":
			creates++
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"id": "batch-1", "status": "queued"})

		case r.Method == "GET" && r.URL.Path == "/batches/batch-1":
			polls++
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{
				"id":             "batch-1",
				"status":         "completed",
				"output_file_id": "file-out",
			})

		case r.Method == "GET" && r.URL.Path == "/files/file-out/content":
			downloads++
			w.Header().Set("Content-Type", "application/jsonl")
			w.Write([]byte(`{"custom_id":"chunk-0","response":{"status_code":200,"body":{"data":[{"embedding":[0.1,0.2],"index":0}]}}}` + "\n"))

		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})

	got, err := c.BatchEmbedAsync([]string{"hello"}, nil)
	if err != nil {
		t.Fatalf("BatchEmbedAsync: %v", err)
	}
	if uploads != 1 || creates != 1 || polls != 1 || downloads != 1 {
		t.Errorf("calls: uploads=%d creates=%d polls=%d downloads=%d, want all 1",
			uploads, creates, polls, downloads)
	}
	want := [][]float32{{0.1, 0.2}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("embeddings = %v, want %v", got, want)
	}
}

func TestBatchEmbedAsyncEmpty(t *testing.T) {
	c := NewClient("https://api.example/v1", "k", "m", 8, TaskPrefix{})
	got, err := c.BatchEmbedAsync(nil, nil)
	if err != nil {
		t.Fatalf("BatchEmbedAsync(nil): %v", err)
	}
	if got != nil {
		t.Errorf("embeddings = %v, want nil", got)
	}
}

func TestPrepareBatchJSONLAppliesDocumentPrefix(t *testing.T) {
	c := NewClient("https://api.example/v1", "k", "m", 4, TaskPrefix{Query: "search_query: ", Document: "search_document: "})
	data, err := c.PrepareBatchJSONL([]string{"hello"})
	if err != nil {
		t.Fatalf("PrepareBatchJSONL: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	var first batchRequestLine
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("unmarshal line 0: %v", err)
	}
	if got := first.Body["input"]; got != "search_document: hello" {
		t.Errorf("body.input = %v, want %q", got, "search_document: hello")
	}
}

func TestPrepareBatchJSONLIdempotent(t *testing.T) {
	c := NewClient("https://api.example/v1", "k", "m", 4, TaskPrefix{Query: "search_query: ", Document: "search_document: "})
	data, err := c.PrepareBatchJSONL([]string{"search_document: already_prefixed", "raw"})
	if err != nil {
		t.Fatalf("PrepareBatchJSONL: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	var line0, line1 batchRequestLine
	json.Unmarshal([]byte(lines[0]), &line0)
	json.Unmarshal([]byte(lines[1]), &line1)

	if line0.Body["input"] != "search_document: already_prefixed" {
		t.Errorf("line0 input = %v, want %q", line0.Body["input"], "search_document: already_prefixed")
	}
	if line1.Body["input"] != "search_document: raw" {
		t.Errorf("line1 input = %v, want %q", line1.Body["input"], "search_document: raw")
	}
}
