package xberg

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/extractor"
)

// Client is an extractor.Extractor backed by a remote xberg serve API.
// It POSTs files as multipart byte uploads (xberg disables local-URI inputs
// by default for safety), and requests markdown output by default so that
// structure is preserved for chunking.
type Client struct {
	baseURL      string
	outputFormat string
	http         *http.Client
	cacheDir     string

	// formatsMu guards formats; formats is lazily fetched from GET /formats on
	// first Supports call and cached so repeated syncs don't re-query.
	formatsMu sync.Mutex
	formats   map[string]bool
	fetched   bool
}

// New builds a Client from config. cfg.XbergBaseURL and cfg.Timeout are
// expected to be non-zero (Load fills defaults); cacheDir is where extracted
// page images would be written (unused for text-only xberg, but kept for
// interface parity with the builtin backend).
func New(cfg config.ExtractorConfig, cacheDir string) (*Client, error) {
	baseURL := strings.TrimRight(cfg.XbergBaseURL, "/")
	if baseURL == "" {
		baseURL = config.DefaultXbergBaseURL
	}
	outputFormat := cfg.OutputFormat
	if outputFormat == "" {
		outputFormat = config.DefaultExtractorOutputFormat
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = config.DefaultXbergTimeout
	}
	return &Client{
		baseURL:      baseURL,
		outputFormat: outputFormat,
		http:         &http.Client{Timeout: timeout},
		cacheDir:     cacheDir,
		formats:      make(map[string]bool),
	}, nil
}

// Name implements extractor.Extractor.
func (c *Client) Name() string { return "xberg" }

// Supports reports whether the file extension is handled by the server. The
// supported set is fetched once from GET /formats and cached. If the fetch
// fails (e.g. server down), Supports returns false so callers skip the file
// rather than attempting a doomed extraction.
func (c *Client) Supports(path string) bool {
	if err := c.ensureFormats(context.Background()); err != nil {
		return false
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext == "" {
		return false
	}
	return c.formats[ext]
}

// Extract uploads the file at path to /extract and returns its text.
func (c *Client) Extract(ctx context.Context, path string) (extractor.Result, error) {
	f, err := os.Open(path)
	if err != nil {
		return extractor.Result{}, fmt.Errorf("xberg: open %s: %w", path, err)
	}
	defer f.Close()

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	// output_format field must precede the file for some servers; xberg reads
	// it from multipart fields regardless of order, but ordering is harmless.
	if err := mw.WriteField("output_format", c.outputFormat); err != nil {
		return extractor.Result{}, fmt.Errorf("xberg: write output_format: %w", err)
	}
	fw, err := mw.CreateFormFile("file", filepath.Base(path))
	if err != nil {
		return extractor.Result{}, fmt.Errorf("xberg: create form file: %w", err)
	}
	if _, err := io.Copy(fw, f); err != nil {
		return extractor.Result{}, fmt.Errorf("xberg: copy file: %w", err)
	}
	if err := mw.Close(); err != nil {
		return extractor.Result{}, fmt.Errorf("xberg: close multipart: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/extract", &body)
	if err != nil {
		return extractor.Result{}, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := c.http.Do(req)
	if err != nil {
		return extractor.Result{}, fmt.Errorf("xberg: request %s: %w", path, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return extractor.Result{}, fmt.Errorf("xberg: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return extractor.Result{}, fmt.Errorf("xberg: %s status %d: %s", path, resp.StatusCode, truncate(string(data), 300))
	}

	var out extractionResult
	if err := json.Unmarshal(data, &out); err != nil {
		return extractor.Result{}, fmt.Errorf("xberg: decode %s: %w", path, err)
	}
	if len(out.Results) == 0 {
		// Surface any per-input error the server reported.
		if len(out.Errors) > 0 {
			return extractor.Result{}, fmt.Errorf("xberg: %s: %s", path, out.Errors[0].Message)
		}
		return extractor.Result{}, fmt.Errorf("xberg: %s: no results", path)
	}
	doc := out.Results[0]
	content := strings.TrimSpace(doc.Content)
	if content == "" {
		return extractor.Result{}, fmt.Errorf("xberg: %s: empty content", path)
	}

	res := extractor.Result{
		Content:  content,
		MimeType: doc.MimeType,
		Title:    titleFromPath(path),
	}
	// Non-fatal per-input errors: log via the result but don't fail — content
	// is present and indexable. Callers can inspect len(out.Errors) if needed;
	// for simplicity we carry on.
	return res, nil
}

// Health pings GET /health. Returns an error if the server is unreachable or
// unhealthy. Callers should invoke this once before a sync so a misconfigured
// server fails fast instead of per-file.
func (c *Client) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("xberg: health check %s: %w (is `xberg serve` running?)", c.baseURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("xberg: health check status %d", resp.StatusCode)
	}
	return nil
}

// ensureFormats fetches the supported extension set once and caches it.
func (c *Client) ensureFormats(ctx context.Context) error {
	c.formatsMu.Lock()
	defer c.formatsMu.Unlock()
	if c.fetched {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/formats", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("xberg: fetch formats: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("xberg: formats status %d", resp.StatusCode)
	}
	var fmts []supportedFormat
	if err := json.NewDecoder(resp.Body).Decode(&fmts); err != nil {
		return fmt.Errorf("xberg: decode formats: %w", err)
	}
	for _, f := range fmts {
		ext := strings.ToLower(f.Extension)
		if ext != "" && !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		if ext != "" {
			c.formats[ext] = true
		}
	}
	c.fetched = true
	return nil
}

// titleFromPath is a fallback title when the server doesn't surface one in
// metadata. The builtin markdown/pdf extractors derive titles similarly.
func titleFromPath(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// Compile-time interface check.
var _ extractor.Extractor = (*Client)(nil)

// NewWithHealthCheck builds a Client and probes GET /health once. It is the
// constructor callers should use when they want a down/misconfigured server to
// surface as a construction-time error rather than silently marking every file
// unsupported during sync. The probe has a short timeout so it doesn't block
// startup for long; a reachable-but-slow server is fine, an unreachable one
// fails fast with an actionable message.
func NewWithHealthCheck(cfg config.ExtractorConfig, cacheDir string) (extractor.Extractor, error) {
	c, err := New(cfg, cacheDir)
	if err != nil {
		return nil, err
	}
	probeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Health(probeCtx); err != nil {
		return nil, err
	}
	return c, nil
}
