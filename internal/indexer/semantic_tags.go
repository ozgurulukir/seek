package indexer

import (
	"context"
	"strings"
	"time"

	"github.com/ozgurulukir/seek/internal/chunk"
	"github.com/ozgurulukir/seek/internal/semantic"
)

// semanticChunkCap bounds how many chunks of one document are sent for
// tagging. Very large documents stop here; the tags of the first chunks
// still represent the document well (keyphrases are stable across a doc).
const semanticChunkCap = 200

// semanticTags returns the deduplicated, capped tag list for text via the
// semantic service, or nil when the capability is disabled, unavailable,
// or the service fails. Semantic enrichment is always optional: it must
// never fail a sync, and keyword search is unaffected either way.
//
// The same chunking pipeline as FTS is used so tags describe exactly what
// is searchable.
func (idx *Indexer) semanticTags(ctx context.Context, label, text string) []string {
	p := idx.semanticProvider()
	if p == nil {
		return nil
	}
	maxSize, _ := idx.chunkSize()
	parts := chunk.ChunkMarkdown(text, maxSize, 0)
	if len(parts) == 0 {
		return nil
	}
	if len(parts) > semanticChunkCap {
		parts = parts[:semanticChunkCap]
	}
	req := semantic.Request{
		MaxTags: idx.cfg.Config.Semantic.EffectiveMaxTags(),
	}
	for i, part := range parts {
		req.Chunks = append(req.Chunks, semantic.Chunk{ID: i, Text: part.Content})
	}

	// A per-sync timeout budget: the service is local, so a healthy one
	// answers in seconds; bound the worst case.
	cctx, cancel := context.WithTimeout(ctx, idx.cfg.Config.Semantic.EffectiveTimeout())
	defer cancel()
	resp, err := p.Tag(cctx, req)
	if err != nil {
		// Degrade: service down is expected during startup/shutdown.
		idx.warnf("  WARN: semantic %s: %v (keyword index continues without tags)\n", label, err)
		return nil
	}

	seen := map[string]bool{}
	var tags []string
	for _, r := range resp.Results {
		for _, t := range r.Tags {
			if t != "" && !seen[t] {
				seen[t] = true
				tags = append(tags, t)
			}
		}
	}
	if len(tags) == 0 {
		return nil
	}
	return tags
}

// semanticTagMap turns a tag list into a fast field (comma-joined, same
// encoding as markdown frontmatter tags).
func semanticTagMap(tags []string) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	return map[string]string{"tags": strings.Join(tags, ",")}
}

// semanticProvider lazily resolves the semantic client from config. The
// first call validates the offline-only gate and checks the service
// health once; a dead service disables the capability for the process
// lifetime (it is re-evaluated on the next process start).
func (idx *Indexer) semanticProvider() semantic.Provider {
	if idx.semChecked {
		return idx.semClient
	}
	idx.semChecked = true

	cfg := idx.cfg
	if cfg == nil || !cfg.Config.Semantic.Enabled {
		return nil
	}
	baseURL := cfg.Config.Semantic.EffectiveBaseURL()
	if err := semantic.ValidateOffline(cfg, baseURL); err != nil {
		idx.warnf("  WARN: semantic disabled: %v\n", err)
		return nil
	}
	client := semantic.NewClient(baseURL, cfg.Config.Semantic.EffectiveTimeout())

	ctx, cancel := context.WithTimeout(idx.ctx(), 10*time.Second)
	defer cancel()
	health, err := client.Health(ctx)
	if err != nil {
		idx.warnf("  WARN: semantic service unavailable at %s (%v) — tags disabled for this run\n", baseURL, err)
		return nil
	}
	caps := []string{}
	if health.Models.LID {
		caps = append(caps, "lid")
	}
	if health.Models.Ner {
		caps = append(caps, "ner")
	}
	if health.Models.Keyphrase {
		caps = append(caps, "keyphrase")
	}
	if health.Models.Topic {
		caps = append(caps, "topic")
	}
	idx.log.Printf("  semantic service: %s (capabilities: %s)\n", baseURL, strings.Join(caps, ", "))
	idx.semClient = client
	return client
}
