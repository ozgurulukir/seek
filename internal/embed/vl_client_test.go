package embed

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestNewVLClientEndpoint(t *testing.T) {
	// Explicit endpoint is used as-is.
	c := NewVLClient("k", "model", 8, "https://provider.example/v1/embeddings", TaskPrefix{})
	if c.endpoint != "https://provider.example/v1/embeddings" {
		t.Errorf("explicit endpoint = %q, want custom", c.endpoint)
	}

	// Empty endpoint falls back to DashScope default.
	d := NewVLClient("k", "model", 8, "", TaskPrefix{})
	if d.endpoint != DefaultVLEndpoint {
		t.Errorf("empty endpoint = %q, want %q", d.endpoint, DefaultVLEndpoint)
	}
}

// newTestVLClient returns a VLClient pointed at a handler that records the
// text of every embedded content and answers with a single embedding.
func newTestVLClient(t *testing.T, texts *[]string) *VLClient {
	t.Helper()
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req vlRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		mu.Lock()
		for _, content := range req.Input.Contents {
			if txt, ok := content["text"]; ok {
				*texts = append(*texts, txt)
			}
		}
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(vlResponse{
			Output: struct {
				Embeddings []struct {
					Embedding []float32 `json:"embedding"`
					Index     int       `json:"index"`
				} `json:"embeddings"`
			}{
				Embeddings: []struct {
					Embedding []float32 `json:"embedding"`
					Index     int       `json:"index"`
				}{{Embedding: []float32{0.5, 0.5}, Index: 0}},
			},
		})
	}))
	t.Cleanup(srv.Close)

	c := NewVLClient("k", "vl-model", 2, srv.URL, TaskPrefix{Query: "search_query: ", Document: "search_document: "})
	c.http = srv.Client()
	return c
}

func TestVLClientTaskPrefixes(t *testing.T) {
	var texts []string
	c := newTestVLClient(t, &texts)

	if _, err := c.EmbedText("query"); err != nil {
		t.Fatalf("EmbedText: %v", err)
	}
	if _, err := c.EmbedBatch([]EmbedItem{{Text: "doc"}}); err != nil {
		t.Fatalf("EmbedBatch: %v", err)
	}

	want := []string{"search_query: query", "search_document: doc"}
	if len(texts) != len(want) {
		t.Fatalf("captured texts = %v, want %v", texts, want)
	}
	for i := range want {
		if texts[i] != want[i] {
			t.Errorf("text[%d] = %q, want %q", i, texts[i], want[i])
		}
	}
}

func TestVLClientZeroPrefixSendsRawText(t *testing.T) {
	var texts []string
	c := newTestVLClient(t, &texts)
	c.taskPrefix = TaskPrefix{}

	if _, err := c.EmbedText("plain query"); err != nil {
		t.Fatalf("EmbedText: %v", err)
	}
	if len(texts) != 1 || texts[0] != "plain query" {
		t.Errorf("texts = %v, want [plain query]", texts)
	}
}

func TestVLClientPureImagePreservesEmptyText(t *testing.T) {
	var texts []string
	c := newTestVLClient(t, &texts)

	// An image with no caption/text should not get a synthetic document prefix text
	if _, err := c.EmbedBatch([]EmbedItem{{ImageURI: "data:image/png;base64,abc"}}); err != nil {
		t.Fatalf("EmbedBatch: %v", err)
	}
	if len(texts) != 0 {
		t.Errorf("expected 0 text fields sent for pure image, got %v", texts)
	}
}

func TestVLClientPartialResponseErrors(t *testing.T) {
	// The helper's server always answers with a single embedding, so a
	// two-item batch is a partial response and must error instead of
	// leaving a nil hole in the batch results.
	var texts []string
	c := newTestVLClient(t, &texts)

	if _, err := c.EmbedBatch([]EmbedItem{{Text: "a"}, {Text: "b"}}); err == nil {
		t.Fatal("expected error for partial VL response")
	} else if !strings.Contains(err.Error(), "returned 1 embeddings, expected 2") {
		t.Errorf("error = %q, want count mismatch mention", err.Error())
	}
}

// TestNewVLClientOfflineRefusesNonLoopback pins the defense-in-depth guard:
// a direct newVLClient call with offline=true and a non-loopback endpoint must
// refuse to send, so privacy.offline_only cannot be bypassed by skipping the
// factory's check.
func TestNewVLClientOfflineRefusesNonLoopback(t *testing.T) {
	c := newVLClient("k", "vl-model", 2, "https://evil.example/v1", TaskPrefix{}, true)
	if !c.refuseNetwork {
		t.Fatal("refuseNetwork = false for non-loopback offline client, want true")
	}
	_, err := c.EmbedText("secret")
	if err == nil {
		t.Fatal("expected refusal, got nil error")
	}
	if !strings.Contains(err.Error(), "offline_only") {
		t.Errorf("error = %q, want offline_only mention", err.Error())
	}
}

// TestNewVLClientOfflineAllowsLoopback verifies the guard does not block a
// numeric loopback endpoint: offline=true against 127.0.0.1 must not be
// refused (it may still succeed or fail to connect, but never with an
// offline_only refusal).
func TestNewVLClientOfflineAllowsLoopback(t *testing.T) {
	var texts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req vlRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		for _, content := range req.Input.Contents {
			if txt, ok := content["text"]; ok {
				texts = append(texts, txt)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(vlResponse{
			Output: struct {
				Embeddings []struct {
					Embedding []float32 `json:"embedding"`
					Index     int       `json:"index"`
				} `json:"embeddings"`
			}{
				Embeddings: []struct {
					Embedding []float32 `json:"embedding"`
					Index     int       `json:"index"`
				}{{Embedding: []float32{0.5, 0.5}, Index: 0}},
			},
		})
	}))
	t.Cleanup(srv.Close)

	c := newVLClient("k", "vl-model", 2, srv.URL, TaskPrefix{}, true)
	if c.refuseNetwork {
		t.Fatal("refuseNetwork = true for loopback offline client, want false")
	}
	c.http = srv.Client()

	if _, err := c.EmbedText("local"); err != nil {
		if strings.Contains(err.Error(), "offline_only") {
			t.Errorf("loopback offline request was refused: %v", err)
		}
	}
}

// TestVLClientSuccessPathStillWorks guards against the offline guard or the
// bounded error-body change regressing the normal online success path.
func TestVLClientSuccessPathStillWorks(t *testing.T) {
	var texts []string
	c := newTestVLClient(t, &texts)
	if _, err := c.EmbedText("hello"); err != nil {
		t.Fatalf("EmbedText: %v", err)
	}
	if len(texts) != 1 || texts[0] != "search_query: hello" {
		t.Errorf("texts = %v, want [search_query: hello]", texts)
	}
}

// TestEmbedImagesInBatchesReportsReadFailures pins the M15 contract: image
// read failures must surface as an error, not as an apparent success with a
// low count — the old code dropped failed reads with a bare continue and
// returned (0, nil) when every image failed (review 2026-09-17 M15).
func TestEmbedImagesInBatchesReportsReadFailures(t *testing.T) {
	c := NewVLClient("k", "model", 8, "https://provider.example/v1/embeddings", TaskPrefix{})
	items := []ImageBatchItem{
		{ImagePath: filepath.Join(t.TempDir(), "missing1.png")},
		{ImagePath: filepath.Join(t.TempDir(), "missing2.png")},
	}
	updated, err := c.EmbedImagesInBatches(items, 5, 0, nil)
	if err == nil {
		t.Fatal("expected read failures to produce an error, got nil")
	}
	if updated != 0 {
		t.Fatalf("expected 0 updated, got %d", updated)
	}
}
