package embed

import (
	"fmt"
	"net/url"
	"strings"
	"sync"

	"github.com/ozgurulukir/seek/internal/config"
)

// ProviderKind classifies the configured embedding endpoint. It is the
// non-secret identity used by the persisted embedding profile (P3): the kind
// is stored instead of the API key or the full URL.
type ProviderKind string

const (
	// ProviderKindGeneric is any OpenAI-compatible endpoint that is not
	// recognized as a local helper or a known hosted provider. It defaults to
	// realtime embeddings and never to async batch.
	ProviderKindGeneric ProviderKind = "generic"
	// ProviderKindOllama is a local Ollama endpoint (loopback, port 11434 or
	// an "ollama" model name). It serves only the synchronous /embeddings
	// surface.
	ProviderKindOllama ProviderKind = "ollama"
	// ProviderKindLocal is a local helper service (tools/embed_server,
	// FastEmbed) on a loopback address. It serves only the synchronous
	// /embeddings surface.
	ProviderKindLocal ProviderKind = "local"
	// ProviderKindHosted is a known hosted provider whose adapter declares
	// its capabilities explicitly (including async batch support).
	ProviderKindHosted ProviderKind = "hosted"
)

func (k ProviderKind) String() string { return string(k) }

// hostedProviderSpec declares the capabilities of a known hosted provider.
// Async batch is only declared for providers that actually expose the
// Files + Batch API; it is never guessed from the URL shape.
type hostedProviderSpec struct {
	hostSuffix string
	asyncBatch bool
}

var hostedProviders = []hostedProviderSpec{
	{"dashscope.aliyuncs.com", true},
	{"api.openai.com", true},
}

// isLocalEndpoint reports whether rawURL points at a loopback address or the
// "localhost" hostname. Hostnames other than localhost are deliberately not
// treated as local so DNS/hosts-file configuration cannot turn a remote
// destination into a local one.
func isLocalEndpoint(rawURL string) bool {
	if config.IsNumericLoopbackURL(rawURL) {
		return true
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Hostname(), "localhost")
}

// DetectProviderKind classifies the configured embedding endpoint. It is used
// both to compute capabilities and to build the persisted embedding profile.
func DetectProviderKind(cfg *config.AppConfig) ProviderKind {
	if cfg == nil {
		return ProviderKindGeneric
	}
	e := cfg.Config.Embedding
	u, err := url.Parse(e.BaseURL)
	if err != nil || u.Hostname() == "" {
		return ProviderKindGeneric
	}
	host := strings.ToLower(u.Hostname())
	for _, hp := range hostedProviders {
		if host == hp.hostSuffix || strings.HasSuffix(host, "."+hp.hostSuffix) {
			return ProviderKindHosted
		}
	}
	if isLocalEndpoint(e.BaseURL) {
		if u.Port() == "11434" || strings.Contains(strings.ToLower(e.Model), "ollama") {
			return ProviderKindOllama
		}
		return ProviderKindLocal
	}
	return ProviderKindGeneric
}

// ProviderCapabilities computes the embedding surface for the configured
// provider. Realtime embeddings are assumed for every provider that exposes
// the OpenAI-compatible /embeddings endpoint. Async batch is declared only by
// known hosted provider adapters; a loopback address never implies it and no
// /files probe is performed (probing is side-effectful and would confuse an
// auth error with a missing capability).
func ProviderCapabilities(cfg *config.AppConfig) Capabilities {
	caps := Capabilities{RealtimeEmbeddings: true}
	if cfg == nil {
		return caps
	}
	u, err := url.Parse(cfg.Config.Embedding.BaseURL)
	if err != nil || u.Hostname() == "" {
		return caps
	}
	host := strings.ToLower(u.Hostname())
	for _, hp := range hostedProviders {
		if host == hp.hostSuffix || strings.HasSuffix(host, "."+hp.hostSuffix) {
			caps.AsyncBatch = hp.asyncBatch
			return caps
		}
	}
	return caps
}

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
	provider, err := factory(cfg)
	if err != nil {
		return Provider{}, err
	}
	return provider.NormalizeCapabilities(), nil
}

func NewProviderFromConfig(cfg *config.AppConfig) (Provider, error) {
	return NewProviderRegistry().Build("configured", cfg)
}

func configuredProvider(cfg *config.AppConfig) (Provider, error) {
	if cfg == nil {
		return Provider{}, fmt.Errorf("embedding provider: nil config")
	}
	p := Provider{}
	if client := NewClientFromConfig(cfg); client != nil {
		// Assign only a real client. Boxing a nil *Client into an interface
		// produces a non-nil typed-nil interface and makes capability checks
		// incorrectly report that embeddings are available.
		p.Query = client
		p.Document = client
		p.Batch = client
	}
	if vl := NewVLClientFromConfig(cfg); vl != nil {
		p.VLQuery = vl
		p.VLText = vl
		p.VLImage = vl
	}
	if cfg.Config.Rerank.Enabled && cfg.Config.Rerank.APIKey != "" &&
		(!cfg.Config.OfflineOnly() || config.IsNumericLoopbackURL(cfg.Config.Rerank.BaseURL)) {
		p.Reranker = newRerankClient(cfg.Config.Rerank.BaseURL, cfg.Config.Rerank.APIKey, cfg.Config.Rerank.Model, cfg.Config.OfflineOnly())
	}
	p.Capabilities = ProviderCapabilities(cfg)
	return p, nil
}

// NewClientFromConfig builds the text-embedding client from config. Under
// privacy.offline_only, numeric loopback endpoints remain usable while all
// other endpoints receive a network-refusing client. It returns nil when no
// API key is configured.
func NewClientFromConfig(cfg *config.AppConfig) *Client {
	if cfg.Config.OfflineOnly() && !config.IsNumericLoopbackURL(cfg.Config.Embedding.BaseURL) {
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
// config. Returns nil when no API key is configured, when the configured model
// is not multimodal, or when offline_only rejects the configured endpoint.
func NewVLClientFromConfig(cfg *config.AppConfig) *VLClient {
	ec := cfg.Config.Embedding
	vlURL := ec.VLBaseURL
	if vlURL == "" {
		vlURL = ec.BaseURL
	}
	if cfg.Config.OfflineOnly() && !config.IsNumericLoopbackURL(vlURL) {
		return nil
	}
	key, err := cfg.RequireEmbeddingKey()
	if err != nil {
		return nil
	}
	// Only create VL client for multimodal models.
	if !ec.IsMultimodal() {
		return nil
	}
	q, d := ec.TaskPrefixes()
	// Use the validated fallback URL as the actual endpoint too. Otherwise an
	// empty vl_base_url would silently select the remote DashScope default.
	return newVLClient(key, ec.Model, ec.Dimensions, vlURL, TaskPrefix{Query: q, Document: d}, cfg.Config.OfflineOnly())
}

// EmbeddingCapability reports whether the embedding pipeline can run for the
// given config, and why not when it cannot. Callers use this to decide
// between embedding work and a keyword-only degraded run: a missing
// capability is a configuration state (skip with a pointer to how to fix
// it), not a runtime failure.
func EmbeddingCapability(cfg *config.AppConfig) (bool, string) {
	if cfg.Config.OfflineOnly() && !config.IsNumericLoopbackURL(cfg.Config.Embedding.BaseURL) {
		return false, "privacy.offline_only blocks the non-loopback embedding endpoint (keyword search unaffected)"
	}
	if _, err := cfg.RequireEmbeddingKey(); err != nil {
		return false, "embedding API key not configured — run: seek auth login (keyword search unaffected)"
	}
	if cfg.Config.Embedding.IsMultimodal() && NewVLClientFromConfig(cfg) == nil {
		return false, "multimodal embedding client unavailable — check embedding.vl_base_url (keyword search unaffected)"
	}
	return true, ""
}
