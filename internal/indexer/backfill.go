package indexer

import (
	"context"
	"fmt"

	"github.com/ozgurulukir/seek/internal/store"
)

// BackfillReport is the outcome accounting of one semantic backfill pass.
// The counts mirror the plan's progress/processed/skipped/failed language and
// are reported per collection.
type BackfillReport struct {
	// Processed counts documents whose semantic state was updated this pass
	// (enrichment succeeded and the fingerprint is now current).
	Processed int
	// Skipped counts documents already enriched to the desired fingerprint
	// — content and enrichment identity unchanged, so no work was needed.
	Skipped int
	// Failed counts documents whose enrichment failed (timeout, malformed
	// response, service down, or a state-write error). Their previously
	// stored fast fields are preserved and the semantic status records the
	// failure.
	Failed int

	// HealthFailed marks a pass that did no work because the semantic
	// service's /health check failed after the provider had already resolved.
	// It keeps the no-op state contract (no writes) but lets callers tell this
	// degraded outcome apart from a genuinely empty backlog (all counts zero,
	// HealthFailed false).
	HealthFailed bool
}

// BackfillSemantic runs a collection-scoped semantic fast-field backfill
// (plan §4 S3 / `reindex --semantic-only`). Every document whose stored
// enrichment fingerprint is missing or no longer matches the desired identity
// and content is re-enriched from its already-indexed chunks — never from
// source files — and its fast fields plus semantic fingerprint/status are
// atomically updated via store.UpdateSemanticState.
//
// Isolation contract (--semantic-only): this pass only READS chunks and fast
// fields, and only WRITES through UpdateSemanticState, which touches exactly
// the fast_fields table plus the semantic_fingerprint/semantic_status columns
// of documents. It does not re-read or rewrite source files, does not touch
// documents_fts or chunk contents, does not write embeddings, and never calls
// SyncVectorIndex / VectorIndex.Clear. Embeddings and the FTS rowset are
// byte-for-byte identical after a backfill.
//
// Degrade-on-failure: an enrichment failure marks the document SemanticStatusError
// with a nil fast-field map (the S2 contract preserves prior fields) and the
// pass continues. Cancellation via ctx stops the loop at the next boundary
// without corrupting state. The caller is responsible for holding the writer
// lock before invoking, matching the existing sync/embed model.
func (idx *Indexer) BackfillSemantic(ctx context.Context, col *store.Collection, log Logger) (BackfillReport, error) {
	var report BackfillReport
	if col == nil {
		return report, fmt.Errorf("semantic backfill: nil collection")
	}
	if idx == nil || idx.db == nil {
		return report, fmt.Errorf("semantic backfill %s: indexer has no store", col.Name)
	}
	if log == nil {
		log = defaultLogger{}
	}

	// One recorder per pass owns the basis resolution and every
	// UpdateSemanticState write below. A fresh instance per pass keeps the
	// sync path's warn-once cadence independent of backfill passes (resolve
	// failures are never memoized, so the next pass re-checks health anyway).
	rec := newSemanticStateRecorder(idx.db, idx.cfg, idx.semanticProvider, nil)
	basis, ok, err := rec.resolve(ctx)
	if err != nil {
		// Transient health failure after the provider already resolved (the
		// service answered at resolution but is not answering /health now):
		// degrade to a no-op pass, same as the unavailable-provider case
		// below. Fingerprinting against an empty-capability basis instead
		// would mismatch every persisted fingerprint and re-select — and with
		// a half-up service, re-enrich — the whole collection, twice (this
		// pass and the next healthy one).
		log.Printf("  WARN: semantic backfill %s: semantic service health check failed (%v) — nothing to backfill\n", col.Name, err)
		report.HealthFailed = true
		return report, nil
	}
	if !ok {
		// Nothing to enrich with: the capability is disabled or the service
		// is unreachable. Warn once and leave every document untouched — the
		// plan keeps existing fields and the stale state instead of mass-
		// failing the collection. When enrichment is enabled but no provider
		// resolves (most commonly the service was down at resolution), flag
		// the pass as degraded so a 0/0/0 no-op is not mistaken for a healthy
		// empty backlog.
		if idx.cfg != nil && idx.cfg.Config.Semantic.Enabled {
			report.HealthFailed = true
		}
		log.Printf("  WARN: semantic backfill %s: semantic enrichment unavailable (disabled or service down) — nothing to backfill\n", col.Name)
		return report, nil
	}

	// The store query takes one desired fingerprint; per-document content
	// hashes differ, so the basis (identity without content) is the coarse
	// candidate selector and the precise per-doc comparison happens below.
	candidates, err := idx.db.GetStaleSemanticDocuments(ctx, col.ID, basis.Compute())
	if err != nil {
		return report, fmt.Errorf("semantic backfill %s: select stale documents: %w", col.Name, err)
	}

	log.Printf("  Backfilling semantic fields for %s: %d candidate document(s)\n", col.Name, len(candidates))

	for _, cand := range candidates {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		if err := idx.backfillDocument(ctx, rec, cand, &report); err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return report, ctxErr
			}
			log.Printf("  WARN: semantic backfill %s doc %d: %v\n", col.Name, cand.DocumentID, err)
			report.Failed++
		}
	}

	log.Printf("  Backfilled semantic fields for %s: %d processed, %d skipped, %d failed\n",
		col.Name, report.Processed, report.Skipped, report.Failed)
	return report, nil
}

// backfillDocument enriches one stale document and persists the outcome. It
// returns an error only when the document could not be refined (enrichment
// failure or persistence failure); the caller counts it as Failed. All
// semantic-state writes route through the pass recorder (rec), which owns
// the basis resolution and the write-side decision table.
func (idx *Indexer) backfillDocument(ctx context.Context, rec *semanticStateRecorder, cand store.SemanticDocumentState, report *BackfillReport) error {
	doc, err := idx.db.GetDocumentByIDContext(ctx, cand.DocumentID)
	if err != nil {
		return fmt.Errorf("load document %d: %w", cand.DocumentID, err)
	}
	chunks, err := idx.db.ListChunksForDocumentContext(ctx, cand.DocumentID)
	if err != nil {
		return fmt.Errorf("load chunks of %s: %w", doc.Path, err)
	}

	// Per-document desired fingerprint: the identity basis plus a content
	// hash over the same readable chunks the semantic service sees. A cached
	// fingerprint that already matches is "current" — nothing to do.
	desiredFP, err := rec.desiredFingerprint(ctx, chunks)
	if err != nil {
		// Unreachable after BackfillSemantic's resolve gate (the basis is
		// memoized for the pass), but guard honestly rather than writing a
		// fingerprint against nothing.
		return fmt.Errorf("semantic basis unresolved for %s: %w", doc.Path, err)
	}
	if cand.Fingerprint == desiredFP && cand.Status == store.SemanticStatusCurrent {
		report.Skipped++
		return nil
	}

	// No readable text (image-only content): there is nothing to enrich.
	// Record the current fingerprint so later passes skip this document
	// without re-reading its chunks, preserving any native fast fields.
	if !hasReadableText(chunks) {
		if err := rec.RecordBackfillCurrent(ctx, cand.DocumentID, doc.ContentHash, chunks, nil); err != nil {
			return fmt.Errorf("finalize empty document %s: %w", doc.Path, err)
		}
		report.Processed++
		return nil
	}

	fields := idx.semanticFastFields(ctx, doc.Path, chunks)
	if fields == nil {
		// Enrichment failed (service down, timeout, malformed response — the
		// no-text case is already excluded above): degrade-on-failure. The
		// recorder stores the FULL desired fingerprint (basis + content hash)
		// and marks the document error: the stale query re-selects every
		// document whose semantic_status = 'error', so this document is
		// retried on the next pass instead of being pinned as a permanent
		// failure. The error status (not the fingerprint) is what guarantees
		// the retry.
		if err := rec.RecordBackfillError(ctx, cand.DocumentID, doc.ContentHash, chunks); err != nil {
			return fmt.Errorf("record enrichment failure for %s: %w", doc.Path, err)
		}
		return fmt.Errorf("enrichment of %s failed (service down, timeout, or malformed response)", doc.Path)
	}

	persisted, err := idx.db.FastFields().ListForDocumentContext(ctx, cand.DocumentID)
	if err != nil {
		return fmt.Errorf("read fast fields of %s: %w", doc.Path, err)
	}
	// The native side of the merge is the set of SOURCE-derived fields only.
	// Previously persisted semantic fields (topics/entities/language) are
	// semantic-owned: on a re-enrichment the freshly computed values must
	// REPLACE them (the exact case the fingerprint invalidation is designed
	// for — a model/capability/content change), so they are excluded from the
	// native map where "native wins on conflict" would retain the stale
	// values. tags stays native: it is dual-owned (markdown frontmatter tags
	// are source-derived) and the S1 merge policy unions it instead.
	native := stripSemanticOwned(persisted)
	merged := mergeFastFields(native, fields)
	if merged == nil {
		// A successful recompute-to-nothing with no native fields remaining
		// (the service returned no tags/topics/entities/language and there is
		// no frontmatter/parser metadata) must still CLEAR previously stored
		// semantic fields: the store contract distinguishes nil (preserve)
		// from a non-nil empty map (explicit clear), so pass the empty map
		// rather than nil, or stale semantic metadata from a prior enrichment
		// would survive a genuine recomputation (review M1).
		merged = map[string]string{}
	}
	if err := rec.RecordBackfillCurrent(ctx, cand.DocumentID, doc.ContentHash, chunks, merged); err != nil {
		return fmt.Errorf("persist enriched fast fields of %s: %w", doc.Path, err)
	}
	report.Processed++
	return nil
}

// semanticOwnedFastFields are the fast-field names produced exclusively by
// the semantic service. When a document is re-enriched, the fresh semantic
// values must replace whatever was previously persisted under these keys, so
// they must never enter the native side of the merge (where "native wins on
// conflict" would retain a stale value from a prior enrichment).
//
// tags is deliberately NOT in this set: it is dual-owned — markdown frontmatter
// provides a native producer — so the S1 merge policy (mergeFastFields) keeps
// the native value in play and unions the semantic tags into it.
var semanticOwnedFastFields = map[string]struct{}{
	semanticFieldTopics:   {},
	semanticFieldEntities: {},
	semanticFieldLanguage: {},
}

// stripSemanticOwned removes the semantic-owned fast fields from a set of
// persisted fields, leaving only the source-derived (native) fields. It
// returns nil when nothing source-derived remains, so mergeFastFields falls
// back to the fresh semantic map alone.
func stripSemanticOwned(persisted map[string]string) map[string]string {
	if len(persisted) == 0 {
		return nil
	}
	native := make(map[string]string, len(persisted))
	for k, v := range persisted {
		if _, owned := semanticOwnedFastFields[k]; owned {
			continue
		}
		native[k] = v
	}
	return native
}
