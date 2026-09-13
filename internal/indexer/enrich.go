package indexer

import (
	"context"
	"strings"

	"github.com/ozgurulukir/seek/internal/store"
)

// DocumentEnricher is the common document-finalization seam every format
// handler routes its fast fields through. It merges native fast fields
// (frontmatter, code metadata, parser metadata) with optional semantic
// enrichment, so handlers do not call the semantic provider directly.
//
// The seam is deliberately behavior-preserving. During a regular sync it
// applies semantic enrichment only to the sync matrix — conversations
// (claude/codex), PDF, and documents. Markdown, code (and any type not on
// the sync matrix, e.g. images or parser collections) pass their native
// fields through unchanged; they reach semantic parity through the
// collection-scoped backfill (`seek collection reindex <name>
// --semantic-only`), which is type-agnostic and re-enriches already-indexed
// chunks without re-reading source files.
type DocumentEnricher interface {
	// Enrich returns the fast fields to persist for one document. native
	// wins on conflict; semantic tags are deduped and merged into the native
	// tags value. The return contract mirrors semanticFastFields:
	//
	//   - nil: enrichment failed or was not attempted (semantic disabled/down,
	//     or no readable text) and there are no native fields — preserve
	//     previously stored fast fields (store nil-map semantics on replace);
	//   - non-nil, even empty: enrichment succeeded, possibly recomputing to
	//     nothing; a non-nil empty map explicitly clears previously stored
	//     semantic fields.
	Enrich(ctx context.Context, colType store.CollectionType, label string, native map[string]string, chunks []store.IndexChunk) map[string]string
}

// indexerEnricher is the default DocumentEnricher backed by the Indexer's
// lazy semantic provider resolution.
type indexerEnricher struct {
	idx *Indexer
}

func (e *indexerEnricher) Enrich(ctx context.Context, colType store.CollectionType, label string, native map[string]string, chunks []store.IndexChunk) map[string]string {
	if !semanticEligible(colType) {
		return native
	}
	semantic := e.idx.semanticFastFields(ctx, label, chunks)
	if semantic == nil {
		// Enrichment failed or was not attempted (service down, disabled, or
		// no readable text): nothing was recomputed, so preserve previously
		// stored fields — returning native (nil here) keeps the writer's
		// nil-map "do not touch" semantics.
		return native
	}
	merged := mergeFastFields(native, semantic)
	if merged == nil {
		// Enrichment SUCCEEDED but recomputed to nothing and there are no
		// native fields to persist. Signal the explicit clear (a non-nil
		// empty map) so the writer drops previously stored semantic fields
		// instead of keeping stale values (review M1).
		return map[string]string{}
	}
	return merged
}

// semanticEligible reports whether a collection type receives semantic
// enrichment during a regular sync. This is the final sync matrix:
// conversations (claude/codex), PDF, and documents are enriched in sync;
// every other type (markdown, code, images, parser) is deliberately NOT —
// they are brought onto the semantic matrix by the type-agnostic
// `seek collection reindex <name> --semantic-only` backfill
// (BackfillSemantic) rather than during sync.
func semanticEligible(colType store.CollectionType) bool {
	switch colType {
	case store.CollectionTypePDF,
		store.CollectionTypeDocuments,
		store.CollectionTypeClaude,
		store.CollectionTypeCodex:
		return true
	default:
		return false
	}
}

// mergeFastFields merges native fast fields with semantic enrichment using
// the plan's explicit policy (§3.1):
//
//   - user/frontmatter value is preserved: native wins on conflict, except
//     for the `tags` field (below);
//   - semantic tags are deduped and merged into the native tags value, so
//     frontmatter tags and generated tags coexist (plan: "semantic tags
//     dedupe edilerek eklenebilir");
//   - code `lang` and semantic `language` are distinct fields (both kept).
//
// Returns nil only when both inputs are nil/empty, so the store's nil-map
// semantics are preserved (a nil result leaves previously stored fast fields
// untouched on replace).
func mergeFastFields(native, semantic map[string]string) map[string]string {
	if len(native) == 0 && len(semantic) == 0 {
		return nil
	}
	if len(native) == 0 {
		return semantic
	}
	if len(semantic) == 0 {
		return native
	}
	merged := make(map[string]string, len(native)+len(semantic))
	for k, v := range native {
		merged[k] = v
	}
	for k, v := range semantic {
		if existing, ok := merged[k]; ok {
			if k == semanticFieldTags {
				merged[k] = mergeTagValues(existing, v)
			}
			// Any other conflicting key keeps the native (user/frontmatter)
			// value per the plan.
			continue
		}
		merged[k] = v
	}
	return merged
}

// mergeTagValues joins two comma-separated tag lists into one deduplicated
// list, preserving first-seen order (native values first, then the new
// semantic values) and trimming whitespace. Empty parts are skipped.
func mergeTagValues(native, semantic string) string {
	seen := make(map[string]bool)
	var out []string
	for _, combine := range [][]string{strings.Split(native, ","), strings.Split(semantic, ",")} {
		for _, tag := range combine {
			tag = strings.TrimSpace(tag)
			if tag == "" || seen[tag] {
				continue
			}
			seen[tag] = true
			out = append(out, tag)
		}
	}
	return strings.Join(out, ",")
}
