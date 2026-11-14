package config

import (
	"fmt"
	"strings"
)

// ServiceStatus classifies one optional helper service for `seek doctor`
// (plan C6 — "opsiyonel servislerin sınırı"). The four states mirror seek's
// "optional services" model: every helper is a capability, never a dependency
// of the core keyword flow.
//
//   - disabled:    the capability is not configured (off by default).
//   - ready:       configured and allowed by the current privacy policy.
//     Readiness is config-derived — doctor never probes the endpoint, so the
//     status is fast and deterministic (see ServiceStatuses).
//   - unavailable: configured but the helper endpoint is down/unreachable.
//     This is intentionally never emitted by ServiceStatuses: doctor avoids
//     network probes by design. It stays in the state space for completeness
//     and any future probe-based reporting.
//   - blocked:     configured but refused by privacy.offline_only (a remote
//     endpoint where only a numeric loopback address is allowed).
type ServiceStatus string

const (
	ServiceDisabled    ServiceStatus = "disabled"
	ServiceReady       ServiceStatus = "ready"
	ServiceUnavailable ServiceStatus = "unavailable"
	ServiceBlocked     ServiceStatus = "blocked"
)

// ServiceReport is one row of the optional-services matrix reported by
// `seek doctor`. Key is a stable identifier used by tests; Name is the label
// shown in output; Detail is a short reason (endpoint, policy, or why disabled).
type ServiceReport struct {
	Key    string
	Name   string
	Status ServiceStatus
	Detail string
}

// ServiceStatuses classifies every optional helper service from the resolved
// config, in a fixed order (reranker, semantic, ocr/vl, xberg). It is a pure,
// read-only projection of cfg — no clients are constructed and no network call
// is made — so it is safe to call from `seek doctor` on every invocation.
//
// Readiness is config-derived: the classification mirrors the exact offline
// policy each capability enforces at construction time (embed.configuredProvider
// for reranker, semantic.NewClientFromConfig for semantic,
// embed.NewVLClientFromConfig for VL, and indexer.NewExtractor for xberg), so
// the reported status matches what a real add/sync/search run would do. Doctor
// stays fast and deterministic by design.
func ServiceStatuses(cfg *AppConfig) []ServiceReport {
	if cfg == nil {
		return nil
	}
	c := &cfg.Config
	offline := c.OfflineOnly()
	return []ServiceReport{
		rerankReport(c, offline),
		semanticReport(c, offline),
		ocrVLReport(c, offline),
		xbergReport(c, offline),
	}
}

// rerankReport classifies the optional cross-encoder reranker. It is configured
// when rerank.enabled is set; offline_only blocks a remote endpoint (mirrors
// embed.configuredProvider, which only installs a reranker for loopback under
// offline_only).
func rerankReport(c *Config, offline bool) ServiceReport {
	if !c.Rerank.Enabled {
		return ServiceReport{"reranker", "reranker", ServiceDisabled, "rerank.enabled not set"}
	}
	if offline && !IsNumericLoopbackURL(c.Rerank.BaseURL) {
		return ServiceReport{"reranker", "reranker", ServiceBlocked,
			fmt.Sprintf("rerank endpoint %q is remote; refused by privacy.offline_only", c.Rerank.BaseURL)}
	}
	return ServiceReport{"reranker", "reranker", ServiceReady,
		fmt.Sprintf("rerank endpoint %s (%s)", c.Rerank.BaseURL, c.Rerank.Model)}
}

// semanticReport classifies the optional local semantic tag service. It is
// configured when semantic.enabled is set; offline_only blocks a remote
// endpoint (mirrors semantic.NewClientFromConfig / ValidateOffline).
func semanticReport(c *Config, offline bool) ServiceReport {
	if !c.Semantic.Enabled {
		return ServiceReport{"semantic", "semantic", ServiceDisabled, "semantic.enabled not set"}
	}
	base := c.Semantic.EffectiveBaseURL()
	if offline && !IsNumericLoopbackURL(base) {
		return ServiceReport{"semantic", "semantic", ServiceBlocked,
			fmt.Sprintf("semantic endpoint %q is remote; refused by privacy.offline_only", base)}
	}
	return ServiceReport{"semantic", "semantic", ServiceReady,
		fmt.Sprintf("semantic endpoint %s", base)}
}

// ocrVLReport classifies the combined OCR + vision-language (VL) embedding
// capability. Either leg being configured counts as "configured"; offline_only
// blocks whichever configured endpoint is remote (mirrors embed.NewOCRClient
// and embed.NewVLClientFromConfig, which both refuse remote endpoints under
// offline_only).
func ocrVLReport(c *Config, offline bool) ServiceReport {
	ocrOn := c.OCR.Enabled && c.OCR.APIKey != ""
	vlOn := c.Embedding.IsMultimodal()
	if !ocrOn && !vlOn {
		return ServiceReport{"ocr-vl", "ocr/vl", ServiceDisabled, "ocr disabled; not multimodal"}
	}
	if offline {
		if ocrOn && !IsNumericLoopbackURL(c.OCR.BaseURL) {
			return ServiceReport{"ocr-vl", "ocr/vl", ServiceBlocked,
				fmt.Sprintf("ocr endpoint %q is remote; refused by privacy.offline_only", c.OCR.BaseURL)}
		}
		if vlOn && !IsNumericLoopbackURL(vlEndpoint(c.Embedding)) {
			return ServiceReport{"ocr-vl", "ocr/vl", ServiceBlocked,
				fmt.Sprintf("vl endpoint %q is remote; refused by privacy.offline_only", vlEndpoint(c.Embedding))}
		}
	}
	parts := make([]string, 0, 2)
	if ocrOn {
		parts = append(parts, "ocr")
	}
	if vlOn {
		parts = append(parts, "vl")
	}
	return ServiceReport{"ocr-vl", "ocr/vl", ServiceReady,
		fmt.Sprintf("%s enabled", strings.Join(parts, ", "))}
}

// xbergReport classifies the remote rich-document extractor. It is configured
// only when extractor.backend == "xberg". offline_only always blocks it — xberg
// POSTs full document contents to a remote service, so the backend is refused
// outright (mirrors indexer.NewExtractor, which errors under offline_only).
//
// Note: selecting xberg is an explicit user request, so add/sync of documents
// is legitimately dependent on the service in this case — unlike the core
// markdown/code/pdf/images lex paths, which run helper-free. That distinction
// is surfaced in the detail string.
func xbergReport(c *Config, offline bool) ServiceReport {
	backend := c.Extractor.Backend
	if backend == "" {
		backend = DefaultExtractorBackend
	}
	if backend != "xberg" {
		return ServiceReport{"xberg", "xberg", ServiceDisabled,
			fmt.Sprintf("extractor.backend=%s (xberg not selected)", backend)}
	}
	if offline {
		return ServiceReport{"xberg", "xberg", ServiceBlocked,
			"xberg sends document contents out; refused by privacy.offline_only (use --backend builtin)"}
	}
	return ServiceReport{"xberg", "xberg", ServiceReady,
		fmt.Sprintf("extractor.backend=xberg → %s (explicit dependency: documents add/sync needs it)", c.Extractor.XbergBaseURL)}
}

// vlEndpoint resolves the effective VL endpoint: the configured vl_base_url, or
// the embedding base_url when unset (mirrors embed.NewVLClientFromConfig).
func vlEndpoint(ec EmbeddingConfig) string {
	if ec.VLBaseURL != "" {
		return ec.VLBaseURL
	}
	return ec.BaseURL
}
