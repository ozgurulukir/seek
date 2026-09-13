package indexer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/ozgurulukir/seek/internal/semantic"
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

	p := idx.semanticProvider()
	if p == nil {
		// Nothing to enrich with: the capability is disabled or the service
		// is unreachable. Warn once and leave every document untouched — the
		// plan keeps existing fields and the stale state instead of mass-
		// failing the collection.
		log.Printf("  WARN: semantic backfill %s: semantic enrichment unavailable (disabled or service down) — nothing to backfill\n", col.Name)
		return report, nil
	}

	basis, err := idx.semanticFingerprintBasis(ctx, p)
	if err != nil {
		return report, fmt.Errorf("semantic backfill %s: %w", col.Name, err)
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
		if err := idx.backfillDocument(ctx, cand, basis, &report); err != nil {
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
// failure or persistence failure); the caller counts it as Failed.
func (idx *Indexer) backfillDocument(ctx context.Context, cand store.SemanticDocumentState, basis store.SemanticFingerprint, report *BackfillReport) error {
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
	desired := basis
	desired.ContentHash = semanticContentHash(chunks)
	desiredFP := desired.Compute()
	if cand.Fingerprint == desiredFP && cand.Status == store.SemanticStatusCurrent {
		report.Skipped++
		return nil
	}

	// No readable text (image-only content): there is nothing to enrich.
	// Record the current fingerprint so later passes skip this document
	// without re-reading its chunks, preserving any native fast fields.
	if !hasReadableText(chunks) {
		if err := idx.db.UpdateSemanticState(ctx, cand.DocumentID, desiredFP, store.SemanticStatusCurrent, nil); err != nil {
			return fmt.Errorf("finalize empty document %s: %w", doc.Path, err)
		}
		report.Processed++
		return nil
	}

	fields := idx.semanticFastFields(ctx, doc.Path, chunks)
	if fields == nil {
		// Enrichment failed (service down, timeout, malformed response, or
		// no readable text — the no-text case is already excluded above):
		// degrade-on-failure. The S2 contract preserves the previously stored
		// fast fields when the map is nil. Store the FULL desired fingerprint
		// (basis + content hash): the stale-selection query compares against
		// the identity-only basis, so this document is re-selected and
		// retried on the next pass, instead of being pinned as a permanent
		// failure.
		if err := idx.db.UpdateSemanticState(ctx, cand.DocumentID, desiredFP, store.SemanticStatusError, nil); err != nil {
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
	if err := idx.db.UpdateSemanticState(ctx, cand.DocumentID, desiredFP, store.SemanticStatusCurrent, merged); err != nil {
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

// semanticFingerprintBasis computes the identity part of the desired
// enrichment fingerprint for one backfill pass: the service deployment
// identity (base URL), the capability set the service reports via /health,
// and the enrichment schema version. Per-document content hashes are layered
// on top by backfillDocument (and recordSemanticSyncState on the sync path).
// A health failure degrades to an empty capability set so the pass can still
// run against a partially-reporting service.
//
// Plan §3.2 calls this "service/model identity". The configured MODEL name is
// deliberately not part of the basis: SemanticConfig has no model selector —
// the semantic service is a single local NLP endpoint (tools/semantic) whose
// runtime models are chosen server-side and published via /health. The base
// URL plus the /health capability set therefore identify the deployment as
// precisely as a model name could. If a client-side model selector is ever
// added to SemanticConfig, it must be folded into this basis so a model
// change invalidates documents (review minor: model-name identity).
func (idx *Indexer) semanticFingerprintBasis(ctx context.Context, p semantic.Provider) (store.SemanticFingerprint, error) {
	basis := store.SemanticFingerprint{
		SchemaVersion: store.SemanticSchemaVersion,
	}
	if idx.cfg == nil {
		basis.ServiceModel = ""
		return basis, nil
	}
	basis.ServiceModel = idx.cfg.Config.Semantic.EffectiveBaseURL()

	hctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	health, err := p.Health(hctx)
	if err != nil {
		return basis, nil // degrade: empty capability set
	}
	basis.Capabilities = store.SemanticCapabilities{
		Language:  health.Models.LID,
		NER:       health.Models.Ner,
		Keyphrase: health.Models.Keyphrase,
		Topic:     health.Models.Topic,
	}
	return basis, nil
}

// semanticSyncBasis returns the semantic enrichment identity for the current
// process, computed lazily and cached. A sync pass records the fingerprint
// once per document, so the identity part (which health-checks the service)
// must be computed once rather than once per document; the per-document
// content hash is layered on top by the caller. WithSemanticProvider resets
// the cache so provider swaps observe the new identity.
func (idx *Indexer) semanticSyncBasis(ctx context.Context) (store.SemanticFingerprint, bool) {
	if idx.semBasisSet {
		return idx.semBasis, true
	}
	p := idx.semanticProvider()
	if p == nil {
		// Disabled or service down: there is no identity to record against.
		// The provider cache itself (semChecked/semClient) is authoritative,
		// so do not memoize this miss.
		return store.SemanticFingerprint{}, false
	}
	basis, err := idx.semanticFingerprintBasis(ctx, p)
	if err != nil {
		return store.SemanticFingerprint{}, false
	}
	idx.semBasis = basis
	idx.semBasisSet = true
	return basis, true
}

// recordSemanticSyncState records the semantic fingerprint/status of one
// document whose sync pass enriched it (plan §3.2). The fast fields were
// already persisted by the replace writer via DocumentIndex.FastFields; this
// only updates the fingerprint/status columns so `collection list/show`
// coverage is accurate after a sync, not just after a backfill.
//
// The fingerprint identity matches backfill: service identity + capability
// set + schema version, with the per-document content hash layered on top.
//
// fastFields is the enricher's output for the document:
//
//   - nil: enrichment failed (service down/malformed) or was not attempted
//     (disabled, no readable text). When semantic enrichment is enabled the
//     document is marked stale (prior fields preserved — the nil map keeps
//     them) so the next backfill retries it; when semantic is disabled
//     entirely, no state is recorded (nothing to be stale about, and the
//     capability is simply off).
//   - non-nil (even empty): enrichment succeeded, possibly recomputing to
//     nothing; the document is marked current with the full desired
//     fingerprint so later passes/backfills skip it.
func (idx *Indexer) recordSemanticSyncState(ctx context.Context, colType store.CollectionType, docID int64, fastFields map[string]string, chunks []store.IndexChunk, label string) error {
	if docID == 0 || !semanticEligible(colType) {
		return nil
	}
	if fastFields == nil {
		if idx.cfg == nil || !idx.cfg.Config.Semantic.Enabled {
			return nil
		}
		// Enabled but the service did not respond (or there was no text to
		// send): record the stale state so the next backfill re-enriches this
		// document. Pass nil fast fields so previously stored semantic fields
		// are preserved.
		return idx.db.UpdateSemanticState(ctx, docID, "", store.SemanticStatusStale, nil)
	}
	basis, ok := idx.semanticSyncBasis(ctx)
	if !ok {
		return nil
	}
	basis.ContentHash = semanticContentHash(chunks)
	return idx.db.UpdateSemanticState(ctx, docID, basis.Compute(), store.SemanticStatusCurrent, nil)
}

// semanticContentHash derives a stable per-document content hash over the
// readable chunk contents the semantic service sees (the same filter
// semanticFastFields applies). NUL separates chunks so adjacent chunks cannot
// merge ambiguously.
func semanticContentHash(chunks []store.IndexChunk) string {
	h := sha256.New()
	sep := []byte{0}
	for _, c := range chunks {
		if strings.TrimSpace(c.Content) == "" {
			continue
		}
		h.Write([]byte(c.Content))
		h.Write(sep)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// hasReadableText reports whether any chunk carries readable text that could
// be sent for semantic enrichment.
func hasReadableText(chunks []store.IndexChunk) bool {
	for _, c := range chunks {
		if strings.TrimSpace(c.Content) != "" {
			return true
		}
	}
	return false
}
