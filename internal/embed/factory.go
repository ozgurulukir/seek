package embed

import (
	"github.com/ozgurulukir/seek/internal/config"
)

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
