package config

import (
	"net/url"
	"strings"
)

// ProviderKind classifies the configured embedding endpoint. It is the
// non-secret identity used by the persisted embedding profile (P3): the kind
// is stored instead of the API key or the full URL.
//
// It lives in config — a leaf package that both embed and store already
// import — so internal/store can derive the embedding profile without
// importing internal/embed (issue #190).
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

// HostedProviderSpec declares the capabilities of a known hosted provider.
// Async batch is only declared for providers that actually expose the
// Files + Batch API; it is never guessed from the URL shape.
type HostedProviderSpec struct {
	HostSuffix string
	AsyncBatch bool
}

// HostedProviders lists the known hosted providers. Exported so internal/embed
// can compute capabilities from the same table that DetectProviderKind uses.
var HostedProviders = []HostedProviderSpec{
	{"dashscope.aliyuncs.com", true},
	{"api.openai.com", true},
}

// isLocalEndpoint reports whether rawURL points at a loopback address or the
// "localhost" hostname. Hostnames other than localhost are deliberately not
// treated as local so DNS/hosts-file configuration cannot turn a remote
// destination into a local one.
func isLocalEndpoint(rawURL string) bool {
	if IsNumericLoopbackURL(rawURL) {
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
func DetectProviderKind(cfg *AppConfig) ProviderKind {
	if cfg == nil {
		return ProviderKindGeneric
	}
	e := cfg.Config.Embedding
	u, err := url.Parse(e.BaseURL)
	if err != nil || u.Hostname() == "" {
		return ProviderKindGeneric
	}
	host := strings.ToLower(u.Hostname())
	for _, hp := range HostedProviders {
		if host == hp.HostSuffix || strings.HasSuffix(host, "."+hp.HostSuffix) {
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
