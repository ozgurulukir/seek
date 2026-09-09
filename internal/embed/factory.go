package embed

import (
	"fmt"
	"sync"

	"github.com/ozgurulukir/seek/internal/config"
)

// ProviderFactory constructs a capability bundle for one configured provider.
type ProviderFactory func(*config.AppConfig) (Provider, error)

// ProviderRegistry keeps provider construction at one seam. Additional
// providers can register a factory without making command or pipeline code
// aware of provider-specific clients.
type ProviderRegistry struct {
	mu        sync.RWMutex
	factories map[string]ProviderFactory
}

func NewProviderRegistry() *ProviderRegistry {
	registry := &ProviderRegistry{factories: make(map[string]ProviderFactory)}
	registry.Register("configured", configuredProvider)
	return registry
}

func (r *ProviderRegistry) Register(name string, factory ProviderFactory) {
	if r == nil || name == "" || factory == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[name] = factory
}

func (r *ProviderRegistry) Build(name string, cfg *config.AppConfig) (Provider, error) {
	if r == nil {
		return Provider{}, fmt.Errorf("embedding provider registry is nil")
	}
	r.mu.RLock()
	factory := r.factories[name]
	r.mu.RUnlock()
	if factory == nil {
		return Provider{}, fmt.Errorf("embedding provider %q is not registered", name)
	}
	return factory(cfg)
}

func NewProviderFromConfig(cfg *config.AppConfig) (Provider, error) {
	return NewProviderRegistry().Build("configured", cfg)
}

func configuredProvider(cfg *config.AppConfig) (Provider, error) {
	if cfg == nil {
		return Provider{}, fmt.Errorf("embedding provider: nil config")
	}
	client := NewClientFromConfig(cfg)
	p := Provider{Query: client, Document: client, Batch: client}
	if vl := NewVLClientFromConfig(cfg); vl != nil {
		p.VLQuery = vl
		p.VLText = vl
		p.VLImage = vl
	}
	if cfg.Config.Rerank.Enabled && cfg.Config.Rerank.APIKey != "" && !cfg.Config.OfflineOnly() {
		p.Reranker = NewRerankClient(cfg.Config.Rerank.BaseURL, cfg.Config.Rerank.APIKey, cfg.Config.Rerank.Model)
	}
	return p, nil
}

// NewClientFromConfig builds the text-embedding client from config. Returns
// the offline (network-refusing) client when privacy.offline_only is set, and
// nil when no API key is configured. Centralizing this here keeps the
// command layer free of provider-construction branches.
func NewClientFromConfig(cfg *config.AppConfig) *Client {
	if cfg.Config.OfflineOnly() {
		return NewOfflineClient(cfg.Config.Embedding.Model)
	}
	key, err := cfg.RequireEmbeddingKey()
	if err != nil {
		return nil
	}
	q, d := cfg.Config.Embedding.TaskPrefixes()
	return NewClient(
		cfg.Config.Embedding.BaseURL,
		key,
		cfg.Config.Embedding.Model,
		cfg.Config.Embedding.Dimensions,
		TaskPrefix{Query: q, Document: d},
	)
}

// NewVLClientFromConfig builds the multimodal (vision+text) client from
// config. Returns nil when offline, when no API key is configured, or when
// the configured model is not multimodal.
func NewVLClientFromConfig(cfg *config.AppConfig) *VLClient {
	if cfg.Config.OfflineOnly() {
		return nil // offline: never build a multimodal network client
	}
	key, err := cfg.RequireEmbeddingKey()
	if err != nil {
		return nil
	}
	ec := cfg.Config.Embedding
	// Only create VL client for multimodal models.
	if !ec.IsMultimodal() {
		return nil
	}
	q, d := ec.TaskPrefixes()
	return NewVLClient(key, ec.Model, ec.Dimensions, ec.VLBaseURL, TaskPrefix{Query: q, Document: d})
}

// EmbeddingCapability reports whether the embedding pipeline can run for the
// given config, and why not when it cannot. Callers use this to decide
// between embedding work and a keyword-only degraded run: a missing
// capability is a configuration state (skip with a pointer to how to fix
// it), not a runtime failure.
func EmbeddingCapability(cfg *config.AppConfig) (bool, string) {
	if cfg.Config.OfflineOnly() {
		return false, "privacy.offline_only is set — embeddings are disabled (keyword search unaffected)"
	}
	if _, err := cfg.RequireEmbeddingKey(); err != nil {
		return false, "embedding API key not configured — run: seek auth login (keyword search unaffected)"
	}
	if cfg.Config.Embedding.IsMultimodal() && NewVLClientFromConfig(cfg) == nil {
		return false, "multimodal embedding client unavailable — check embedding.vl_base_url (keyword search unaffected)"
	}
	return true, ""
}
