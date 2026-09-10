package indexer

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ozgurulukir/seek/internal/semantic"
	"github.com/ozgurulukir/seek/internal/store"
)

// semanticChunkCap bounds how many chunks of one document are sent for
// tagging. Very large documents stop here; the tags of the first chunks
// still represent the document well (keyphrases are stable across a doc).
const semanticChunkCap = 200

// semantic enrichment fast fields.
const (
	semanticFieldTags     = "tags"
	semanticFieldTopics   = "topics"
	semanticFieldEntities = "entities"
	semanticFieldLanguage = "language"
)

// semanticFastFields returns the fast fields produced by the semantic
// service for one document, or nil when the capability is disabled,
// unavailable, or the service fails. Semantic enrichment is always optional:
// it must never fail a sync, and keyword search is unaffected either way.
//
// chunks must be the same chunks written to the FTS index (per-page / per-
// section), so the service sees exactly what is searchable and topics fit
// over real document segments (not a re-joined single blob). Returned fields:
//
//	tags     — keyphrases + topic labels (same encoding as markdown frontmatter)
//	topics   — the document's topic labels
//	entities — "TYPE:Text" pairs from NER (e.g. "ORG:OpenAI,LOC:Go")
//	language — detected ISO 639-1 for the document
func (idx *Indexer) semanticFastFields(ctx context.Context, label string, chunks []store.IndexChunk) map[string]string {
	if idx.cfg == nil {
		return nil
	}
	p := idx.semanticProvider()
	if p == nil {
		return nil
	}
	// Chunks carry extracted text regardless of their storage type: PDF
	// pages are rasterized to PNG (ChunkTypeImage) but their Content holds
	// the page text when present. Skip only chunks with no readable text —
	// an image-only page whose Content is empty/whitespace adds no semantic
	// signal and would pollute keyphrase/topic/entity extraction.
	var parts []store.IndexChunk
	for _, c := range chunks {
		if strings.TrimSpace(c.Content) == "" {
			continue
		}
		parts = append(parts, c)
	}
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

	// A per-request timeout budget: the service is local, so a healthy one
	// answers in seconds; bound the worst case.
	cctx, cancel := context.WithTimeout(ctx, idx.cfg.Config.Semantic.EffectiveTimeout())
	defer cancel()
	resp, err := p.Tag(cctx, req)
	if err != nil {
		// Degrade: service down is expected during startup/shutdown.
		idx.warnf("  WARN: semantic %s: %v (keyword index continues without tags)\n", label, err)
		return nil
	}

	fields := map[string]string{}

	tagSeen := map[string]bool{}
	var tags []string
	topicSeen := map[string]bool{}
	var topics []string
	entitySeen := map[string]bool{}
	var entities []string

	for _, r := range resp.Results {
		for _, t := range r.Tags {
			if t != "" && !tagSeen[t] {
				tagSeen[t] = true
				tags = append(tags, t)
			}
		}
		for _, t := range r.Topics {
			label := strings.TrimSpace(t.Label)
			if label == "" || topicSeen[label] {
				continue
			}
			topicSeen[label] = true
			topics = append(topics, label)
		}
		for _, e := range r.Entities {
			pair := fmt.Sprintf("%s:%s", e.Type, e.Text)
			if pair != "" && !entitySeen[pair] {
				entitySeen[pair] = true
				entities = append(entities, pair)
			}
		}
	}

	if len(tags) > 0 {
		fields[semanticFieldTags] = strings.Join(tags, ",")
	}
	if len(topics) > 0 {
		sort.Strings(topics)
		fields[semanticFieldTopics] = strings.Join(topics, ",")
	}
	if len(entities) > 0 {
		sort.Strings(entities)
		fields[semanticFieldEntities] = strings.Join(entities, ",")
	}
	if lang := strings.TrimSpace(resp.CorpusLang); lang != "" && lang != "unknown" {
		fields[semanticFieldLanguage] = lang
	}
	return fields
}

// semanticProvider lazily resolves the semantic client from config. The
// offline gate runs and the service health is checked once; a dead service
// disables the capability for the process lifetime (re-evaluated on the
// next process start).
func (idx *Indexer) semanticProvider() semantic.Provider {
	if idx.semChecked {
		return idx.semClient
	}
	idx.semChecked = true

	if idx.cfg == nil {
		return nil
	}
	if !idx.cfg.Config.Semantic.Enabled {
		return nil
	}
	baseURL := idx.cfg.Config.Semantic.EffectiveBaseURL()
	if err := semantic.ValidateOffline(idx.cfg, baseURL); err != nil {
		idx.warnf("  WARN: semantic disabled: %v\n", err)
		return nil
	}
	client := semantic.NewClient(baseURL, idx.cfg.Config.Semantic.EffectiveTimeout())

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
