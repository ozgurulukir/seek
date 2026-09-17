package config

// Display redaction for credential-bearing config sections. Load() expands
// ${VAR} keys in place, so any path that renders the resolved structs as YAML
// would print the literal secret. Profile rendering (seek config, seek doctor
// --verbose) goes through Redacted copies so the key is always masked
// (review 2026-09-17 M5).

// MaskKey masks a secret for terminal display, keeping enough of the value to
// recognize which credential is configured without exposing it to scrollback
// or shared session logs.
func MaskKey(key string) string {
	if key == "" {
		return ""
	}
	if len(key) <= 8 {
		return "********"
	}
	return key[:4] + "..." + key[len(key)-4:]
}

// Redacted returns a display copy with the API key masked. It is for output
// only — the returned value must never be persisted or used to build clients.
func (e EmbeddingConfig) Redacted() EmbeddingConfig {
	e.APIKey = MaskKey(e.APIKey)
	return e
}

// Redacted returns a display copy with the API key masked (see
// EmbeddingConfig.Redacted).
func (o OCRConfig) Redacted() OCRConfig {
	o.APIKey = MaskKey(o.APIKey)
	return o
}

// Redacted returns a display copy with the API key masked (see
// EmbeddingConfig.Redacted).
func (r RerankConfig) Redacted() RerankConfig {
	r.APIKey = MaskKey(r.APIKey)
	return r
}
