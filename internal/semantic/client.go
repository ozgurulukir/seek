// Package semantic is the seek-side client for the optional local semantic
// tag service (tools/semantic). It follows the OCR extractor pattern: a
// stable JSON envelope over HTTP with a provider-agnostic interface, so the
// service implementation can change (local NLP, another host, another
// runtime) without touching the consumer (see AGENTS.md, "External /
// optional services").
//
// The service is optional: NewClientFromConfig returns nil when the
// capability is disabled, and callers must treat a missing client or a
// failed /tag request as a no-op (keyword search is unaffected).
package semantic

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

// MaxTagsHardLimit mirrors the service-side cap; requests above it are
// rejected by the contract.
const MaxTagsHardLimit = 20

// Chunk is one piece of text to tag. ID is echoed back by the service.
// Lang is an optional ISO 639-1 hint; when empty the service runs language
// detection.
type Chunk struct {
	ID   int    `json:"id"`
	Text string `json:"text"`
	// Lang is empty for detected language.
	Lang string `json:"lang,omitempty"`
}

// Entity is a named entity detected in a chunk.
type Entity struct {
	Text string `json:"text"`
	Type string `json:"type"`
}

// Topic is a topic label with its score.
type Topic struct {
	Label string  `json:"label"`
	Score float64 `json:"score"`
}

// TagResult is the tagged output for one chunk.
type TagResult struct {
	ID       int      `json:"id"`
	Tags     []string `json:"tags"`
	Topics   []Topic  `json:"topics"`
	Entities []Entity `json:"entities"`
}

// Request is the /tag request envelope.
type Request struct {
	Chunks []Chunk `json:"chunks"`
	// MaxTags caps tags per chunk (1..MaxTagsHardLimit; 0 → service default).
	MaxTags int `json:"max_tags,omitempty"`
}

// tagError reports a per-chunk failure; the rest of the batch still returns.
type tagError struct {
	ID      int    `json:"id"`
	Message string `json:"message"`
}

// Response is the /tag response envelope.
type Response struct {
	Results []TagResult `json:"results"`
	Errors  []tagError  `json:"errors"`
}

// Models reports which service capabilities are active.
type Models struct {
	LID       bool `json:"lid"`
	Ner       bool `json:"ner"`
	Keyphrase bool `json:"keyphrase"`
	Topic     bool `json:"topic"`
}

// Health is the /health response.
type Health struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	Models  Models `json:"models"`
}

// Provider is the consumer-facing contract. Implementations are
// interchangeable: switch via config (backend + base_url), not code.
type Provider interface {
	// Tag sends chunks to the service and returns per-chunk results.
	// Results are keyed by chunk ID; missing IDs were skipped (empty text)
	// or failed. A transport-level error (service down) is returned as err.
	Tag(ctx context.Context, req Request) (Response, error)
	// Health checks the service and reports active capabilities.
	Health(ctx context.Context) (Health, error)
}

// Client is the HTTP implementation of Provider against a base_url.
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient builds a Provider against baseURL with the given timeout.
func NewClient(baseURL string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = config.DefaultSemanticTimeout
	}
	return &Client{
		baseURL: baseURL,
		http: &http.Client{
			// Local endpoints must not be able to redirect requests to an
			// external host.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
			Timeout: timeout,
		},
	}
}

// NewClientFromConfig builds a Provider from config, or nil when the
// capability is disabled. Callers must nil-check: semantic is optional.
func NewClientFromConfig(cfg *config.AppConfig) *Client {
	if cfg == nil || !cfg.Config.Semantic.Enabled {
		return nil
	}
	return NewClient(cfg.Config.Semantic.EffectiveBaseURL(), cfg.Config.Semantic.EffectiveTimeout())
}

// Health reports the service's status and active capabilities.
func (c *Client) Health(ctx context.Context) (Health, error) {
	var h Health
	if err := c.do(ctx, http.MethodGet, "/health", nil, &h); err != nil {
		return Health{}, fmt.Errorf("semantic: health check: %w (is the semantic service running?)", err)
	}
	return h, nil
}

// Tag sends the request envelope and returns the parsed response.
func (c *Client) Tag(ctx context.Context, req Request) (Response, error) {
	var out Response
	if err := c.do(ctx, http.MethodPost, "/tag", req, &out); err != nil {
		return out, err
	}
	return out, nil
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("semantic: marshal: %w", err)
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("semantic: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("semantic: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("semantic: %s %s: %s: %s", method, path, resp.Status, string(msg))
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("semantic: decode: %w", err)
		}
	}
	return nil
}

// ValidateOffline reports an error when offline-only mode is active and
// baseURL is not a numeric loopback endpoint. Mirrors the OCR rule: local
// model servers on loopback are trusted; remote endpoints are refused.
func ValidateOffline(cfg *config.AppConfig, baseURL string) error {
	if cfg == nil || !cfg.Config.OfflineOnly() {
		return nil
	}
	if !config.IsNumericLoopbackURL(baseURL) {
		return fmt.Errorf("semantic: base_url %q is not a numeric loopback endpoint; refused under privacy.offline_only", baseURL)
	}
	return nil
}

var _ Provider = (*Client)(nil)
