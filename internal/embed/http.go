package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ozgurulukir/seek/internal/config"
)

// maxErrorBody bounds how many bytes of a non-OK response body are read and
// echoed into error messages. This prevents large or hostile responses from
// being dumped verbatim into errors/logs (potential secret/response leakage).
const maxErrorBody = 2 << 10 // 2KiB

// maxResponseBody bounds how many bytes of a successful (HTTP 200) JSON
// response body are read before an explicit error. Bounded request shapes
// (realtime batches of 25, VL batches by config) cap legitimate responses at
// ~20 MB, so 64 MiB leaves >3x headroom while a misconfigured or hostile
// endpoint can no longer exhaust memory on the success path. The async batch
// result download does NOT go through postJSON because legitimate batch
// results are larger; it uses its own maxBatchResponseBytes cap below.
const maxResponseBody = 64 << 20 // 64 MiB

// maxBatchResponseBytes bounds successful async batch HTTP response bodies
// (upload, create, poll, and result download). Legitimate batch results are
// larger than realtime responses, so this sits above maxResponseBody, but it
// is still bounded so a misconfigured or hostile endpoint cannot exhaust
// memory on the success path.
const maxBatchResponseBytes = 256 << 20 // 256 MiB

// readLimitedBody reads at most limit bytes from r, plus one extra byte to
// detect overflow, and returns a clear error when the body exceeds limit. It
// is the bounded-success-path read shared by the async batch HTTP calls;
// opName names the operation in errors. limit is normally a named constant —
// tests pass a small limit to exercise the overflow branch cheaply.
func readLimitedBody(r io.Reader, limit int64, opName string) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read %s response: %w", opName, err)
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("%s response exceeds %d byte limit", opName, limit)
	}
	return body, nil
}

// boundedErrorSnippet returns at most the first maxErrorBody bytes of an
// already-read response body, for safe inclusion in error messages.
func boundedErrorSnippet(body []byte) string {
	if len(body) > maxErrorBody {
		body = body[:maxErrorBody]
	}
	return string(body)
}

// postJSON is the single HTTP envelope shared by the package's clients:
// marshal req, POST it as JSON with a Bearer Authorization header, require
// HTTP 200, and unmarshal the body into resp. Transport and status errors
// carry opName ("embedding", "vl", "ocr", "rerank") as their prefix; non-200
// bodies are capped at maxErrorBody so provider output can't flood logs, and
// 200 bodies are capped at maxResponseBody so a misconfigured endpoint can't
// exhaust memory. An empty apiKey omits the Authorization header (open endpoints).
func postJSON(ctx context.Context, httpClient *http.Client, endpoint, apiKey, opName string, req, resp any) error {
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}

	respMsg, err := httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("%s request: %w", opName, err)
	}
	defer respMsg.Body.Close()

	if respMsg.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(respMsg.Body, maxErrorBody))
		return fmt.Errorf("%s API %d: %s", opName, respMsg.StatusCode, string(snippet))
	}

	respBody, err := io.ReadAll(io.LimitReader(respMsg.Body, maxResponseBody+1))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if len(respBody) > maxResponseBody {
		return fmt.Errorf("%s API response exceeds %d MiB limit", opName, maxResponseBody>>20)
	}
	if err := json.Unmarshal(respBody, resp); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}
	return nil
}

// indexedEmbedding is the one-entry shape shared by the OpenAI-compatible
// text and DashScope VL embedding response formats.
type indexedEmbedding struct {
	Embedding []float32 `json:"embedding"`
	Index     int       `json:"index"`
}

// reorderEmbeddings re-orders provider-returned embeddings by their index
// field into a dense result slice of length n. Count mismatches, out-of-range
// indexes, and duplicate/missing entries all error: duplicates would satisfy
// a count check while leaving nil holes. opName prefixes the errors so the
// messages name the caller ("embedding", "vl").
func reorderEmbeddings(opName string, entries []indexedEmbedding, n int) ([][]float32, error) {
	if len(entries) != n {
		return nil, fmt.Errorf("%s API returned %d embeddings, expected %d", opName, len(entries), n)
	}
	result := make([][]float32, n)
	for _, e := range entries {
		if e.Index < 0 || e.Index >= n {
			return nil, fmt.Errorf("%s API returned index %d out of range (expected 0..%d)", opName, e.Index, n-1)
		}
		result[e.Index] = e.Embedding
	}
	for i, emb := range result {
		if len(emb) == 0 {
			return nil, fmt.Errorf("%s API returned no embedding for input %d", opName, i)
		}
	}
	return result, nil
}

// newHTTPClient applies the shared transport policy for this package:
//
//   - offline mode (privacy.offline_only) must never egress: no environment
//     proxy, no redirects.
//   - a numeric loopback endpoint gets the same isolation: ProxyFromEnvironment
//     only special-cases the hostname "localhost", so 127.0.0.1/[::1] URLs
//     could otherwise ride HTTP_PROXY, and a loopback service must not be able
//     to redirect private payloads to an external host.
//
// Remote endpoints keep Go's normal proxy and redirect behavior.
func newHTTPClient(timeout time.Duration, offline bool, endpoint string) *http.Client {
	client := &http.Client{Timeout: timeout}
	if !offline && !config.IsNumericLoopbackURL(endpoint) {
		return client
	}
	transport, ok := http.DefaultTransport.(*http.Transport)
	if ok {
		transport = transport.Clone()
	} else {
		transport = &http.Transport{}
	}
	transport.Proxy = nil
	client.Transport = transport
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return client
}
